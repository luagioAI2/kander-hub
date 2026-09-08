package web

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/dualface/kander/internal/board"
)

// defaultPort is the bind port for `kander req serve` when --port is omitted.
const defaultPort = 8940

// PortOr parses raw and falls back to fallback when it is empty or invalid.
func PortOr(raw string, fallback int) int {
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > 65535 {
		return fallback
	}
	return port
}

func init() {
	// The web package registers itself as the `kander req serve` backend. It
	// imports board for storage, so board keeps only a hook variable and
	// neither package ends up in an import cycle.
	board.RunWebServe = runServe
}

// runServe implements `kander req serve`: it binds the combined
// requirements + kanban web UI and blocks until interrupted.
func runServe(args []string) int {
	values := map[string]string{}
	var err error
	args, values["--port"], _, err = board.TakeValueFlag(args, "--port")
	if err != nil {
		board.ReqUsageFail("serve", "board.option_requires_a_value", "--port")
		return 2
	}
	args, values["--host"], _, err = board.TakeValueFlag(args, "--host")
	if err != nil {
		board.ReqUsageFail("serve", "board.option_requires_a_value", "--host")
		return 2
	}
	if len(args) > 0 {
		board.ReqUsageFail("serve", "board.too_many_arguments")
		return 2
	}
	root, err := board.RequireBoardRoot()
	if err != nil {
		board.Fail(err)
		return 1
	}
	port := PortOr(values["--port"], defaultPort)
	host := values["--host"]
	if host == "" {
		host = "127.0.0.1"
	}
	server := &Server{Root: root}
	addr := net.JoinHostPort(host, itoa(port))
	fmt.Println(board.Text("board.web_serving", addr))
	if err = http.ListenAndServe(addr, server.Handler()); err != nil {
		board.Fail(err)
		return 1
	}
	return 0
}

// itoa renders an int as a decimal string.
func itoa(v int) string {
	return strconv.Itoa(v)
}
