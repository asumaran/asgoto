package main

// Focusing any pane (shell, process or agent) through herdr's socket API,
// for tools whose rows are not all agents.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// focusPane focuses an arbitrary pane (shell, process or agent) through
// herdr's socket API (`pane.focus`, newline-delimited JSON on
// HERDR_SOCKET_PATH). The CLI has no equivalent: `pane focus` is
// direction-only and `agent focus <paneID>` rejects non-agent panes since
// herdr 0.9 (agent_not_found), which used to leave shell/process rows dead.
func focusPane(paneID string) error {
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" {
		return fmt.Errorf("HERDR_SOCKET_PATH not set")
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	req, err := paneFocusRequest(paneID)
	if err != nil {
		return err
	}
	if _, err := conn.Write(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// paneFocusRequest encodes one `pane.focus` request line for the socket API.
func paneFocusRequest(paneID string) ([]byte, error) {
	req := struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}{ID: "pane.focus", Method: "pane.focus", Params: map[string]string{"pane_id": paneID}}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
