// Command webrtc-soak runs deterministic, real-Pion WebRTC stability
// scenarios against the production nativewebrtc endpoint state machine.
package main

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/santaklouse/go-p2p-netcat/nativewebrtc"
)

const (
	service             = uint16(31337)
	connectionTimeout   = 15 * time.Second
	reconnectTimeout    = 15 * time.Second
	signalingTopic      = "p2p-netcat-webrtc-soak-v1"
	serverSignalingID   = "SOAKSERVER0000000001"
	clientSignalingID   = "SOAKCLIENT0000000001"
	chunkBytes          = 64 * 1024
	defaultReportSchema = 1
)

type profile struct {
	Iterations   int
	PayloadBytes int
}

var profiles = map[string]profile{
	"smoke": {Iterations: 1, PayloadBytes: 64 * 1024},
	"ci":    {Iterations: 2, PayloadBytes: 512 * 1024},
	"soak":  {Iterations: 12, PayloadBytes: 8 * 1024 * 1024},
}

type adapterConfig struct {
	ID             string
	Name           string
	Latency        time.Duration
	Jitter         time.Duration
	OfferDelay     time.Duration
	Unavailable    bool
	DuplicateRate  float64
	DuplicateTypes map[string]bool
}

type scenario struct {
	Name      string
	Reconnect bool
	Adapters  []adapterConfig
}

var scenarios = []scenario{
	{
		Name: "nostr-trickle",
		Adapters: []adapterConfig{{
			ID: "nostr", Name: "Native Nostr", Latency: 2 * time.Millisecond,
			Jitter: 14 * time.Millisecond, OfferDelay: 20 * time.Millisecond,
		}},
	},
	{
		Name: "torrent-full-sdp",
		Adapters: []adapterConfig{{
			ID: "torrent", Name: "Native BitTorrent", Latency: 3 * time.Millisecond,
			Jitter: 6 * time.Millisecond,
		}},
	},
	{
		Name: "parallel-race",
		Adapters: []adapterConfig{
			{
				ID: "nostr", Name: "Native Nostr", Latency: 2 * time.Millisecond,
				Jitter: 18 * time.Millisecond, OfferDelay: 16 * time.Millisecond,
				DuplicateRate: 1, DuplicateTypes: map[string]bool{"offer": true},
			},
			{
				ID: "torrent", Name: "Native BitTorrent", Latency: 6 * time.Millisecond,
				Jitter: 8 * time.Millisecond, DuplicateRate: 1,
				DuplicateTypes: map[string]bool{"offer": true},
			},
		},
	},
	{
		Name: "adapter-outage",
		Adapters: []adapterConfig{
			{ID: "nostr-offline", Name: "Native Nostr offline", Unavailable: true},
			{
				ID: "torrent", Name: "Native BitTorrent", Latency: 4 * time.Millisecond,
				Jitter: 5 * time.Millisecond,
			},
		},
	},
	{
		Name: "reconnect-same-stream", Reconnect: true,
		Adapters: []adapterConfig{{
			ID: "nostr", Name: "Native Nostr", Latency: 2 * time.Millisecond,
			Jitter: 12 * time.Millisecond, OfferDelay: 14 * time.Millisecond,
		}},
	},
}

type options struct {
	Profile        string
	Iterations     int
	PayloadBytes   int
	Report         string
	Scenarios      []string
	ScenarioFilter bool
}

type signalStats struct {
	Published       int `json:"published"`
	Candidates      int `json:"candidates"`
	Duplicates      int `json:"duplicates"`
	PublishFailures int `json:"publishFailures"`
}

type iterationResult struct {
	Scenario         string       `json:"scenario"`
	Iteration        int          `json:"iteration"`
	Status           string       `json:"status"`
	DurationMS       int64        `json:"durationMs,omitempty"`
	TransferredBytes int64        `json:"transferredBytes,omitempty"`
	Signaling        *signalStats `json:"signaling,omitempty"`
	Strategy         string       `json:"strategy,omitempty"`
	Error            string       `json:"error,omitempty"`
}

type soakReport struct {
	SchemaVersion int               `json:"schemaVersion"`
	StartedAt     string            `json:"startedAt"`
	Environment   reportEnvironment `json:"environment"`
	Configuration reportConfig      `json:"configuration"`
	Results       []iterationResult `json:"results"`
	FinishedAt    string            `json:"finishedAt"`
	Summary       reportSummary     `json:"summary"`
}

type reportEnvironment struct {
	Go           string `json:"go"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
}

type reportConfig struct {
	Profile      string   `json:"profile"`
	Iterations   int      `json:"iterations"`
	PayloadBytes int      `json:"payloadBytes"`
	Scenarios    []string `json:"scenarios"`
}

type reportSummary struct {
	Passed           int   `json:"passed"`
	Failed           int   `json:"failed"`
	TransferredBytes int64 `json:"transferredBytes"`
	DurationMS       int64 `json:"durationMs"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			printHelp(stdout)
			return 0
		}
	}
	parsed, err := parseArguments(arguments, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "[soak] fatal: %v\n", err)
		return 1
	}
	if err := execute(parsed, stdout); err != nil {
		fmt.Fprintf(stderr, "[soak] fatal: %v\n", err)
		return 1
	}
	return 0
}

func parseArguments(arguments []string, output io.Writer) (options, error) {
	if len(arguments) != 0 && arguments[0] == "--" {
		arguments = arguments[1:]
	}
	set := flag.NewFlagSet("webrtc-soak", flag.ContinueOnError)
	set.SetOutput(output)
	set.Usage = func() { printHelp(output) }
	profileName := set.String("profile", "smoke", "workload profile")
	iterations := set.Int("iterations", 0, "override iterations per scenario")
	payloadBytes := set.Int("payload-bytes", 0, "override payload size per direction")
	reportPath := set.String("report", "", "write a JSON report")
	scenarioNames := set.String("scenarios", "", "comma-separated scenario names")
	if err := set.Parse(arguments); err != nil {
		return options{}, err
	}
	if set.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(set.Args(), " "))
	}
	selectedProfile, ok := profiles[*profileName]
	if !ok {
		return options{}, fmt.Errorf(
			"unknown profile %q; expected smoke, ci, or soak", *profileName,
		)
	}
	changed := make(map[string]bool)
	set.Visit(func(value *flag.Flag) { changed[value.Name] = true })
	if changed["iterations"] && *iterations < 1 ||
		changed["payload-bytes"] && *payloadBytes < 1 {
		return options{}, errors.New("--iterations and --payload-bytes must be positive integers")
	}
	if *iterations == 0 {
		*iterations = selectedProfile.Iterations
	}
	if *payloadBytes == 0 {
		*payloadBytes = selectedProfile.PayloadBytes
	}
	selectedNames := splitNames(*scenarioNames)
	if changed["scenarios"] && len(selectedNames) == 0 {
		return options{}, errors.New("no soak scenarios selected")
	}
	return options{
		Profile: *profileName, Iterations: *iterations, PayloadBytes: *payloadBytes,
		Report: *reportPath, Scenarios: selectedNames, ScenarioFilter: changed["scenarios"],
	}, nil
}

func printHelp(output io.Writer) {
	fmt.Fprint(output, "Usage: go run ./cmd/webrtc-soak [options]\n\n"+
		"Options:\n"+
		"  --profile <smoke|ci|soak>  Workload profile (default: smoke)\n"+
		"  --iterations <count>       Override iterations per scenario\n"+
		"  --payload-bytes <bytes>    Override payload size per direction\n"+
		"  --scenarios <a,b>          Run only named scenarios\n"+
		"  --report <file>            Write a JSON report\n")
}

func splitNames(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func execute(opts options, stdout io.Writer) error {
	selected, err := selectScenarios(opts.Scenarios, opts.ScenarioFilter)
	if err != nil {
		return err
	}
	report := soakReport{
		SchemaVersion: defaultReportSchema,
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Environment: reportEnvironment{
			Go: runtime.Version(), Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		},
		Configuration: reportConfig{
			Profile: opts.Profile, Iterations: opts.Iterations,
			PayloadBytes: opts.PayloadBytes, Scenarios: scenarioNames(selected),
		},
		Results: make([]iterationResult, 0, len(selected)*opts.Iterations),
	}
	fmt.Fprintf(
		stdout, "[soak] profile=%s iterations=%d payload=%d bytes/direction\n",
		opts.Profile, opts.Iterations, opts.PayloadBytes,
	)
	for _, selectedScenario := range selected {
		for iteration := 1; iteration <= opts.Iterations; iteration++ {
			fmt.Fprintf(
				stdout, "[soak] %s %d/%d ... ",
				selectedScenario.Name, iteration, opts.Iterations,
			)
			entry := iterationResult{
				Scenario: selectedScenario.Name, Iteration: iteration, Status: "passed",
			}
			result, runErr := runScenarioIteration(selectedScenario, iteration, opts.PayloadBytes)
			if runErr != nil {
				entry.Status = "failed"
				entry.Error = runErr.Error()
				report.Summary.Failed++
				fmt.Fprintf(stdout, "failed (%v)\n", runErr)
			} else {
				entry.DurationMS = result.DurationMS
				entry.TransferredBytes = result.TransferredBytes
				entry.Signaling = &result.Signaling
				entry.Strategy = result.Strategy
				report.Summary.Passed++
				report.Summary.TransferredBytes += result.TransferredBytes
				report.Summary.DurationMS += result.DurationMS
				fmt.Fprintf(stdout, "passed (%d ms, %s)\n", result.DurationMS, result.Strategy)
			}
			report.Results = append(report.Results, entry)
		}
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if opts.Report != "" {
		path, err := writeReport(opts.Report, report)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "[soak] report=%s\n", path)
	}
	fmt.Fprintf(
		stdout, "[soak] passed=%d failed=%d\n",
		report.Summary.Passed, report.Summary.Failed,
	)
	if report.Summary.Failed != 0 {
		return fmt.Errorf("%d WebRTC soak iteration(s) failed", report.Summary.Failed)
	}
	return nil
}

func selectScenarios(names []string, filtered bool) ([]scenario, error) {
	if !filtered {
		return append([]scenario(nil), scenarios...), nil
	}
	if len(names) == 0 {
		return nil, errors.New("no soak scenarios selected")
	}
	known := make(map[string]scenario, len(scenarios))
	for _, value := range scenarios {
		known[value.Name] = value
	}
	selected := make([]scenario, 0, len(names))
	var unknown []string
	for _, name := range names {
		value, ok := known[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		selected = append(selected, value)
	}
	if len(unknown) != 0 {
		return nil, fmt.Errorf("unknown soak scenarios: %s", strings.Join(unknown, ", "))
	}
	if len(selected) == 0 {
		return nil, errors.New("no soak scenarios selected")
	}
	return selected, nil
}

func scenarioNames(values []scenario) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.Name
	}
	return result
}

type scenarioRun struct {
	DurationMS       int64
	TransferredBytes int64
	Signaling        signalStats
	Strategy         string
}

func runScenarioIteration(value scenario, iteration, payloadBytes int) (scenarioRun, error) {
	started := time.Now()
	bus := newSignalingBus(hashSeed(fmt.Sprintf("%s:%d", value.Name, iteration)))
	defer bus.Close()
	privateKey, _, err := crypto.GenerateEd25519Key(cryptorand.Reader)
	if err != nil {
		return scenarioRun{}, err
	}
	serverPeerID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return scenarioRun{}, err
	}
	serverSessions := make([]nativewebrtc.SignalingSession, 0, len(value.Adapters))
	clientSessions := make([]nativewebrtc.SignalingSession, 0, len(value.Adapters))
	for _, adapter := range value.Adapters {
		serverSessions = append(serverSessions, bus.CreateSession(adapter, serverSignalingID))
		clientSessions = append(clientSessions, bus.CreateSession(adapter, clientSignalingID))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := make(chan *nativewebrtc.Stream, 4)
	listener, err := nativewebrtc.StartListenerWithSignalingSessions(
		ctx, privateKey, service, []string{}, serverSessions,
		func(stream *nativewebrtc.Stream, _ string) { accepted <- stream },
	)
	if err != nil {
		return scenarioRun{}, err
	}
	defer listener.Close()
	connection, err := nativewebrtc.ConnectWithSignalingSessions(
		ctx, serverPeerID, service, connectionTimeout, []string{}, clientSessions,
	)
	if err != nil {
		return scenarioRun{}, err
	}
	defer connection.Close()
	serverStream, err := waitForStream(ctx, accepted, connectionTimeout)
	if err != nil {
		return scenarioRun{}, err
	}
	transferred := int64(0)
	if err := runBidirectionalTransfer(
		ctx, connection.Stream, serverStream, payloadBytes,
		hashSeed(fmt.Sprintf("%s:%d:initial", value.Name, iteration)),
	); err != nil {
		return scenarioRun{}, err
	}
	transferred += int64(payloadBytes) * 2
	if value.Reconnect {
		clientStream := connection.Stream
		reconnectCtx, reconnectCancel := context.WithTimeout(ctx, reconnectTimeout)
		err = connection.Reconnect(reconnectCtx)
		reconnectCancel()
		if err != nil {
			return scenarioRun{}, fmt.Errorf("reconnect same stream: %w", err)
		}
		if connection.Stream != clientStream {
			return scenarioRun{}, errors.New("reconnect replaced the client logical stream")
		}
		select {
		case extra := <-accepted:
			return scenarioRun{}, fmt.Errorf("reconnect created a second server stream: %p", extra)
		default:
		}
		if err := runBidirectionalTransfer(
			ctx, connection.Stream, serverStream, payloadBytes,
			hashSeed(fmt.Sprintf("%s:%d:resumed", value.Name, iteration)),
		); err != nil {
			return scenarioRun{}, err
		}
		transferred += int64(payloadBytes) * 2
	}
	stats := bus.Stats()
	return scenarioRun{
		DurationMS: time.Since(started).Milliseconds(), TransferredBytes: transferred,
		Signaling: stats, Strategy: connection.Signaling,
	}, nil
}

func waitForStream(
	ctx context.Context,
	streams <-chan *nativewebrtc.Stream,
	timeout time.Duration,
) (*nativewebrtc.Stream, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case stream := <-streams:
		return stream, nil
	case <-timer.C:
		return nil, fmt.Errorf("server stream timed out after %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func runBidirectionalTransfer(
	parent context.Context,
	client, server *nativewebrtc.Stream,
	payloadBytes int,
	seed uint32,
) error {
	ctx, cancel := context.WithTimeout(parent, connectionTimeout)
	defer cancel()
	clientPayload := deterministicPayload(payloadBytes, seed)
	serverPayload := deterministicPayload(payloadBytes, seed^0xa5a5a5a5)
	results := make(chan error, 2)
	go func() { results <- transferAndVerify(ctx, client, server, clientPayload) }()
	go func() { results <- transferAndVerify(ctx, server, client, serverPayload) }()
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return fmt.Errorf("payload transfer timed out after %s", connectionTimeout)
		}
	}
	return nil
}

func transferAndVerify(
	ctx context.Context,
	sender io.Writer,
	receiver io.Reader,
	payload []byte,
) error {
	actual := make([]byte, len(payload))
	readResult := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(receiver, actual)
		readResult <- err
	}()
	for offset := 0; offset < len(payload); offset += chunkBytes {
		end := min(len(payload), offset+chunkBytes)
		count, err := sender.Write(payload[offset:end])
		if err != nil {
			return err
		}
		if count != end-offset {
			return io.ErrShortWrite
		}
	}
	select {
	case err := <-readResult:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	if !bytes.Equal(sha256Digest(payload), sha256Digest(actual)) {
		return fmt.Errorf(
			"payload hash mismatch: expected %x, received %x",
			sha256Digest(payload), sha256Digest(actual),
		)
	}
	return nil
}

func deterministicPayload(byteLength int, seed uint32) []byte {
	payload := make([]byte, byteLength)
	state := seed
	for index := range payload {
		state = state*1_103_515_245 + 12_345
		payload[index] = byte(state >> 24)
	}
	return payload
}

func sha256Digest(value []byte) []byte {
	digest := sha256.Sum256(value)
	return digest[:]
}

func hashSeed(value string) uint32 {
	digest := sha256.Sum256([]byte(value))
	return uint32(digest[0])<<24 | uint32(digest[1])<<16 |
		uint32(digest[2])<<8 | uint32(digest[3])
}

func writeReport(name string, report soakReport) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

type signalingBus struct {
	mu       sync.Mutex
	sessions map[*memorySession]struct{}
	timers   []*time.Timer
	random   uint32
	stats    signalStats
	closed   atomic.Bool
}

func newSignalingBus(seed uint32) *signalingBus {
	return &signalingBus{
		sessions: make(map[*memorySession]struct{}), random: seed,
	}
}

func (bus *signalingBus) CreateSession(adapter adapterConfig, peerID string) *memorySession {
	session := &memorySession{
		bus: bus, adapter: adapter, peerID: peerID,
		events: make(chan nativewebrtc.Signal, 128),
	}
	bus.mu.Lock()
	bus.sessions[session] = struct{}{}
	bus.mu.Unlock()
	return session
}

func (bus *signalingBus) Publish(
	ctx context.Context,
	sender *memorySession,
	signal nativewebrtc.Signal,
) error {
	bus.mu.Lock()
	if bus.closed.Load() || sender.closed.Load() {
		bus.mu.Unlock()
		return errors.New("signaling session is closed")
	}
	if sender.adapter.Unavailable {
		bus.stats.PublishFailures++
		bus.mu.Unlock()
		return fmt.Errorf("%s is unavailable in this soak scenario", sender.adapter.Name)
	}
	bus.stats.Published++
	if signal.Type == "candidate" {
		bus.stats.Candidates++
	}
	signal.Version = nativewebrtc.SignalVersion
	signal.Room = signalingTopic
	signal.From = sender.peerID
	signal.CreatedAt = time.Now().UnixMilli()
	targets := make([]*memorySession, 0, len(bus.sessions))
	for target := range bus.sessions {
		if target != sender && target.adapter.ID == sender.adapter.ID &&
			(signal.To == "" || signal.To == target.peerID) {
			targets = append(targets, target)
		}
	}
	slices.SortFunc(targets, func(left, right *memorySession) int {
		return strings.Compare(left.peerID, right.peerID)
	})
	for _, target := range targets {
		bus.scheduleLocked(target, signal, sender.adapter, 0)
		if sender.adapter.DuplicateTypes[signal.Type] && sender.adapter.DuplicateRate > 0 &&
			bus.nextRandomLocked() < sender.adapter.DuplicateRate {
			bus.stats.Duplicates++
			bus.scheduleLocked(target, signal, sender.adapter, time.Millisecond)
		}
	}
	bus.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (bus *signalingBus) scheduleLocked(
	target *memorySession,
	signal nativewebrtc.Signal,
	adapter adapterConfig,
	extra time.Duration,
) {
	jitter := time.Duration(0)
	if adapter.Jitter > 0 {
		jitter = time.Duration(bus.nextRandomLocked() * float64(adapter.Jitter))
	}
	delay := adapter.Latency + jitter + extra
	if signal.Type == "offer" {
		delay += adapter.OfferDelay
	}
	timer := time.AfterFunc(delay, func() { target.Deliver(signal) })
	bus.timers = append(bus.timers, timer)
}

func (bus *signalingBus) nextRandomLocked() float64 {
	bus.random = bus.random*1_664_525 + 1_013_904_223
	return float64(bus.random) / float64(uint64(1)<<32)
}

func (bus *signalingBus) Stats() signalStats {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return bus.stats
}

func (bus *signalingBus) remove(session *memorySession) {
	bus.mu.Lock()
	delete(bus.sessions, session)
	bus.mu.Unlock()
}

func (bus *signalingBus) Close() {
	if !bus.closed.CompareAndSwap(false, true) {
		return
	}
	bus.mu.Lock()
	for _, timer := range bus.timers {
		timer.Stop()
	}
	clear(bus.sessions)
	bus.mu.Unlock()
}

type memorySession struct {
	bus     *signalingBus
	adapter adapterConfig
	peerID  string
	events  chan nativewebrtc.Signal
	closed  atomic.Bool
}

func (session *memorySession) Name() string { return session.adapter.Name }

func (session *memorySession) PeerID() string { return session.peerID }

func (session *memorySession) Events() <-chan nativewebrtc.Signal { return session.events }

func (session *memorySession) Status() (int, int) {
	if session.closed.Load() || session.adapter.Unavailable {
		return 0, 1
	}
	return 1, 1
}

func (session *memorySession) Publish(ctx context.Context, signal nativewebrtc.Signal) error {
	return session.bus.Publish(ctx, session, signal)
}

func (session *memorySession) Close() error {
	if session.closed.CompareAndSwap(false, true) {
		session.bus.remove(session)
	}
	return nil
}

func (session *memorySession) Deliver(signal nativewebrtc.Signal) {
	if session.closed.Load() || session.bus.closed.Load() {
		return
	}
	select {
	case session.events <- signal:
	default:
	}
}
