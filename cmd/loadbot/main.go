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

func requestCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
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

func (b *bot) readUntil(want ...string) envelope {
	timeout := time.After(20 * time.Second)
	for {
		select {
		case env := <-b.in:
			if slices.Contains(want, env.Type) {
				return env
			}
		case err := <-b.err:
			panic(fmt.Sprintf("[%s] read %v: %v", b.label, want, err))
		case <-timeout:
			panic(fmt.Sprintf("[%s] never received %v", b.label, want))
		}
	}
}

func (b *bot) waitForAny(want ...string) (envelope, bool) {
	timeout := time.After(20 * time.Second)
	for {
		select {
		case env := <-b.in:
			if slices.Contains(want, env.Type) {
				return env, env.Type != "game_over"
			}
		case err := <-b.err:
			panic(fmt.Sprintf("[%s] read %v: %v", b.label, want, err))
		case <-timeout:
			panic(fmt.Sprintf("[%s] never received %v", b.label, want))
		}
	}
}

func main() {
	addr := flag.String("addr", "localhost:8080", "server address")
	rooms := flag.Int("rooms", 10, "concurrent rooms")
	perRoom := flag.Int("per-room", 3, "players per room")
	verbose := flag.Bool("v", false, "verbose output (print real-time match events)")
	flag.Parse()

	start := time.Now()
	failures := make([]string, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < *rooms; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("room %d: %v", n, rec))
					mu.Unlock()
				}
			}()
			if err := playMatch(*addr, n, *perRoom, *verbose); err != nil {
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
		return err
	}

	if verbose {
		fmt.Printf("\n[Room %s] Created! Joining %d bots...\n", code, perRoom)
	}

	bots := make([]*bot, perRoom)
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

	host.send(envelope{Version: 1, Type: "start_game"})

	guesses := 0
	for _, b := range bots {
		env := b.readUntil("game_start", "error")
		if env.Type == "error" {
			return fmt.Errorf("%s: %s", b.label, env.Payload)
		}
	}

	roundsPlayed := 0
	for {
		rs, isRound := bots[0].waitForAny("round_start", "game_over")
		if !isRound {
			break
		}
		roundsPlayed++
		var p struct {
			Round       int    `json:"round"`
			TotalRounds int    `json:"total_rounds"`
			PanoID      string `json:"pano_id"`
		}
		json.Unmarshal(rs.Payload, &p)

		if verbose {
			fmt.Printf("\n  ┌─ [Round %d/%d] Panorama: %s\n", p.Round, p.TotalRounds, p.PanoID)
		}

		for _, b := range bots[1:] {
			b.readUntil("round_start")
		}
		time.Sleep(time.Duration(rand.IntN(150)+50) * time.Millisecond)
		for _, b := range bots {
			lat := -50.0 + rand.Float64()*115.0
			lng := -170.0 + rand.Float64()*340.0
			body, _ := json.Marshal(map[string]float64{"lat": lat, "lng": lng})
			b.send(envelope{Version: 1, Type: "guess", Payload: body})
			b.readUntil("guess_ack")
			guesses++
			if verbose {
				fmt.Printf("  │  -> [%s] guessed (%.4f, %.4f)\n", b.label, lat, lng)
			}
		}

		var lastResult envelope
		for _, b := range bots {
			lastResult = b.readUntil("round_result")
		}

		if verbose {
			var rr struct {
				Target struct {
					Lat   float64 `json:"lat"`
					Lng   float64 `json:"lng"`
					Title string  `json:"title"`
				} `json:"target"`
				Results []struct {
					Nickname  string  `json:"nickname"`
					DistanceM float64 `json:"distance_m"`
					Score     int     `json:"score"`
				} `json:"results"`
			}
			json.Unmarshal(lastResult.Payload, &rr)
			title := ""
			if rr.Target.Title != "" {
				title = fmt.Sprintf("(%s)", rr.Target.Title)
			}
			fmt.Printf("  └─ [Round %d Reveal] Target: (%.4f, %.4f) %s\n",
				p.Round, rr.Target.Lat, rr.Target.Lng, title)
			for _, res := range rr.Results {
				fmt.Printf("       ★ %-10s: %5d pts (dist: %8.1f km)\n",
					res.Nickname, res.Score, res.DistanceM/1000.0)
			}
		}
	}

	var lastEnd envelope
	for _, b := range bots[1:] {
		lastEnd, _ = b.waitForAny("game_over")
	}

	if verbose {
		var goPayload struct {
			Standings []struct {
				Nickname string `json:"nickname"`
				Total    int    `json:"total"`
			} `json:"standings"`
		}
		json.Unmarshal(lastEnd.Payload, &goPayload)
		fmt.Printf("\n[Room %s] 🏆 Final Standings:\n", code)
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

func createRoom(addr, nickname string) (string, error) {
	body := strings.NewReader(fmt.Sprintf(`{"nickname":%q}`, nickname))
	resp, err := http.Post("http://"+addr+"/v1/rooms", "application/json", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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
	conn, _, err := websocket.Dial(ctx,
		fmt.Sprintf("ws://%s/v1/ws?code=%s&name=%s", addr, code, name), nil)
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
	joined := b.readUntil("joined")
	var p struct {
		Host bool `json:"host"`
	}
	json.Unmarshal(joined.Payload, &p)
	b.isHost = p.Host
	return b, nil
}

func (b *bot) send(env envelope) {
	data, _ := json.Marshal(env)
	ctx, cancel := requestCtx()
	defer cancel()
	if err := b.conn.Write(ctx, websocket.MessageText, data); err != nil {
		panic(fmt.Sprintf("[%s] write: %v", b.label, err))
	}
}
