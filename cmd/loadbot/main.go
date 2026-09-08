package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "server address")
	rooms := flag.Int("rooms", 10, "concurrent rooms")
	perRoom := flag.Int("per-room", 3, "players per room")
	verbose := flag.Bool("v", false, "verbose output (print real-time match events; defaults to true for 1 room)")
	flag.Parse()

	isVerbose := *verbose
	if !isVerbose && *rooms == 1 {
		isVerbose = true
	}

	start := time.Now()
	failures := make([]string, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < *rooms; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := playMatch(*addr, n, *perRoom, isVerbose); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("room %d: %v", n, err))
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	elapsed := time.Since(start)
	fmt.Printf("\n%d rooms x %d bots in %s — %d failures\n",
		*rooms, *perRoom, elapsed.Round(time.Millisecond), len(failures))
	for _, f := range failures {
		fmt.Println("FAIL:", f)
	}
	if len(failures) > 0 {
		os.Exit(1)
	}
}

func playMatch(addr string, n, perRoom int, verbose bool) error {
	code, err := createRoom(addr, fmt.Sprintf("host-%d", n))
	if err != nil {
		return fmt.Errorf("create room: %w", err)
	}

	if verbose {
		fmt.Printf("\n[Room %s] Created! Joining %d bots...\n", code, perRoom)
	}

	bots := make([]*bot, perRoom)
	defer func() {
		for _, b := range bots {
			if b != nil && b.conn != nil {
				_ = b.conn.CloseNow()
			}
		}
	}()

	var firstErr error
	var once sync.Once
	report := func(err error) {
		once.Do(func() { firstErr = err })
	}

	var wg sync.WaitGroup
	for i := range perRoom {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := join(addr, code, fmt.Sprintf("room%d-p%d", n, i))
			if err != nil {
				report(err)
				return
			}
			bots[i] = b
		}(i)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}

	host := bots[0]
	for _, b := range bots {
		if b.isHost {
			host = b
			break
		}
	}

	if verbose {
		fmt.Printf("[Room %s] All bots connected. %s starting match...\n", code, host.label)
	}

	if err := host.send(envelope{Version: 1, Type: "start_game"}); err != nil {
		return fmt.Errorf("host start game: %w", err)
	}

	for _, b := range bots {
		if _, err := b.readUntil("game_start"); err != nil {
			return fmt.Errorf("%s waiting for game_start: %w", b.label, err)
		}
	}

	var lastEnd envelope
	roundsPlayed := 0
	guesses := 0

	for {
		rs, isRound, err := bots[0].waitForAny("round_start", "game_over")
		if err != nil {
			return fmt.Errorf("bot 0 wait round_start/game_over: %w", err)
		}
		if !isRound {
			lastEnd = rs
			break
		}
		roundsPlayed++
		var p struct {
			Round       int    `json:"round"`
			TotalRounds int    `json:"total_rounds"`
			PanoID      string `json:"pano_id"`
		}
		_ = json.Unmarshal(rs.Payload, &p)

		if verbose {
			fmt.Printf("\n  ┌─ [Round %d/%d] Panorama: %s\n", p.Round, p.TotalRounds, p.PanoID)
		}

		for _, b := range bots[1:] {
			if _, err := b.readUntil("round_start"); err != nil {
				return fmt.Errorf("%s read round_start: %w", b.label, err)
			}
		}

		time.Sleep(time.Duration(rand.IntN(100)+30) * time.Millisecond)

		for _, b := range bots {
			lat := -50.0 + rand.Float64()*115.0
			lng := -170.0 + rand.Float64()*340.0
			body, _ := json.Marshal(map[string]float64{"lat": lat, "lng": lng})
			if err := b.send(envelope{Version: 1, Type: "guess", Payload: body}); err != nil {
				return fmt.Errorf("%s send guess: %w", b.label, err)
			}
			if _, err := b.readUntil("guess_ack"); err != nil {
				return fmt.Errorf("%s wait guess_ack: %w", b.label, err)
			}
			guesses++
			if verbose {
				fmt.Printf("  │  -> [%s] guessed (%.4f, %.4f)\n", b.label, lat, lng)
			}
		}

		var lastResult envelope
		for _, b := range bots {
			res, err := b.readUntil("round_result")
			if err != nil {
				return fmt.Errorf("%s wait round_result: %w", b.label, err)
			}
			lastResult = res
		}

		if verbose {
			var rr struct {
				Target struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"target"`
				Results []struct {
					Nickname  string  `json:"nickname"`
					DistanceM float64 `json:"distance_m"`
					Score     int     `json:"score"`
				} `json:"results"`
			}
			_ = json.Unmarshal(lastResult.Payload, &rr)
			fmt.Printf("  └─ [Round %d Reveal] Target: (%.4f, %.4f)\n",
				p.Round, rr.Target.Lat, rr.Target.Lng)
			for _, res := range rr.Results {
				fmt.Printf("       ★ %-10s: %5d pts (dist: %8.1f km)\n",
					res.Nickname, res.Score, res.DistanceM/1000.0)
			}
		}
	}

	for _, b := range bots[1:] {
		_, _, _ = b.waitForAny("game_over")
	}

	if verbose {
		var goPayload struct {
			GameID    string `json:"game_id"`
			Standings []struct {
				Nickname string `json:"nickname"`
				Total    int    `json:"total"`
			} `json:"standings"`
		}
		_ = json.Unmarshal(lastEnd.Payload, &goPayload)
		fmt.Printf("\n[Room %s] Final Standings (Match ID: %s):\n", code, goPayload.GameID)
		for rank, s := range goPayload.Standings {
			winnerBadge := ""
			if rank == 0 {
				winnerBadge = " (WINNER)"
			}
			fmt.Printf("  %d. %-10s: %5d pts%s\n", rank+1, s.Nickname, s.Total, winnerBadge)
		}
		fmt.Println()
	}

	if !verbose {
		fmt.Printf("room %02d complete (%d rounds, %d guesses)\n", n, roundsPlayed, guesses)
	}
	return nil
}

type envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type bot struct {
	conn   *websocket.Conn
	label  string
	isHost bool
	in     chan envelope
	err    chan error
}

func (b *bot) send(env envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	ctx, cancel := requestCtx()
	defer cancel()
	return b.conn.Write(ctx, websocket.MessageText, data)
}

func (b *bot) readLoop() {
	for {
		_, data, err := b.conn.Read(context.Background())
		if err != nil {
			select {
			case b.err <- err:
			default:
			}
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err == nil {
			b.in <- env
		}
	}
}

func (b *bot) readUntil(want ...string) (envelope, error) {
	timeout := time.After(20 * time.Second)
	for {
		select {
		case env := <-b.in:
			if slices.Contains(want, env.Type) {
				return env, nil
			}
			if env.Type == "error" {
				return env, fmt.Errorf("server error: %s", string(env.Payload))
			}
		case err := <-b.err:
			return envelope{}, fmt.Errorf("read error: %w", err)
		case <-timeout:
			return envelope{}, fmt.Errorf("timeout waiting for %v", want)
		}
	}
}

func (b *bot) waitForAny(want ...string) (envelope, bool, error) {
	timeout := time.After(20 * time.Second)
	for {
		select {
		case env := <-b.in:
			if slices.Contains(want, env.Type) {
				return env, env.Type != "game_over", nil
			}
			if env.Type == "error" {
				return env, false, fmt.Errorf("server error: %s", string(env.Payload))
			}
		case err := <-b.err:
			return envelope{}, false, fmt.Errorf("read error: %w", err)
		case <-timeout:
			return envelope{}, false, fmt.Errorf("timeout waiting for %v", want)
		}
	}
}

func createRoom(addr, nickname string) (string, error) {
	ctx, cancel := requestCtx()
	defer cancel()
	body := strings.NewReader(fmt.Sprintf(`{"nickname":%q}`, nickname))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/v1/rooms", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var out struct {
		JoinCode string `json:"join_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.JoinCode, nil
}

func join(addr, code, name string) (*bot, error) {
	ctx, cancel := requestCtx()
	defer cancel()
	conn, resp, err := websocket.Dial(ctx,
		fmt.Sprintf("ws://%s/v1/ws?code=%s&name=%s", addr, code, name), nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	b := &bot{
		conn:  conn,
		label: name,
		in:    make(chan envelope, 64),
		err:   make(chan error, 1),
	}
	go b.readLoop()
	joined, err := b.readUntil("joined")
	if err != nil {
		_ = conn.CloseNow()
		return nil, fmt.Errorf("wait for joined: %w", err)
	}
	var p struct {
		Host bool `json:"host"`
	}
	_ = json.Unmarshal(joined.Payload, &p)
	b.isHost = p.Host
	return b, nil
}

func requestCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}
