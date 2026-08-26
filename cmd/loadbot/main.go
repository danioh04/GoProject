package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
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
	Token   string          `json:"token,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type bot struct {
	conn   *websocket.Conn
	label  string
	token  string
	isHost bool
}

func (b *bot) readUntil(want ...string) envelope {
	for i := 0; i < 40; i++ {
		ctx, cancel := requestCtx()
		_, data, err := b.conn.Read(ctx)
		cancel()
		if err != nil {
			panic(fmt.Sprintf("[%s] read %v: %v", b.label, want, err))
		}
		var env envelope
		json.Unmarshal(data, &env)
		for _, w := range want {
			if env.Type == w {
				return env
			}
		}
	}
	panic(fmt.Sprintf("[%s] never received %v", b.label, want))
}

func main() {
	addr := flag.String("addr", "localhost:8080", "server address")
	rooms := flag.Int("rooms", 10, "concurrent rooms")
	perRoom := flag.Int("per-room", 3, "players per room")
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
			if err := playMatch(*addr, n, *perRoom); err != nil {
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

func playMatch(addr string, n, perRoom int) error {
	code, err := createRoom(addr, fmt.Sprintf("host-%d", n))
	if err != nil {
		return err
	}

	bots := make([]*bot, perRoom)
	var firstErr error
	var once sync.Once
	report := func(err error) {
		once.Do(func() { firstErr = err })
	}

	var wg sync.WaitGroup
	for i := 0; i < perRoom; i++ {
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
	host.send(envelope{Version: 1, Type: "start_game", Token: host.token})

	guesses := 0
	for _, b := range bots {
		env := b.readUntil("game_start", "error")
		if env.Type == "error" {
			return fmt.Errorf("%s: %s", b.label, env.Payload)
		}
	}

	roundsPlayed := 0
	for {
		rs, isRound := waitForAny(bots[0], "round_start", "game_over")
		if !isRound {
			break
		}
		roundsPlayed++
		var p struct {
			Round int `json:"round"`
		}
		json.Unmarshal(rs.Payload, &p)

		for _, b := range bots[1:] {
			b.readUntil("round_start")
		}
		time.Sleep(time.Duration(rand.IntN(200)) * time.Millisecond)
		for _, b := range bots {
			lat := -50 + rand.Float64()*115
			lng := -170 + rand.Float64()*350
			body, _ := json.Marshal(map[string]float64{"lat": lat, "lng": lng})
			b.send(envelope{Version: 1, Type: "guess", Token: b.token, Payload: body})
			b.readUntil("guess_ack")
			guesses++
		}
		for _, b := range bots {
			b.readUntil("round_result")
		}
	}

	for _, b := range bots[1:] {
		waitForAny(b, "game_over")
	}
	fmt.Printf("room %02d complete (%d rounds, %d guesses)\n", n, roundsPlayed, guesses)
	return nil
}

func waitForAny(b *bot, types ...string) (envelope, bool) {
	for i := 0; i < 60; i++ {
		ctx, cancel := requestCtx()
		_, data, err := b.conn.Read(ctx)
		cancel()
		if err != nil {
			panic(fmt.Sprintf("[%s] read %v: %v", b.label, types, err))
		}
		var env envelope
		json.Unmarshal(data, &env)
		for _, w := range types {
			if env.Type == w {
				return env, env.Type != "game_over"
			}
		}
	}
	panic(fmt.Sprintf("[%s] never received %v", b.label, types))
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
	b := &bot{conn: conn, label: name}
	joined := b.readUntil("joined")
	var p struct {
		Token string `json:"token"`
		Host  bool   `json:"host"`
	}
	json.Unmarshal(joined.Payload, &p)
	b.token = p.Token
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
