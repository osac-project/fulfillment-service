/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"fmt"
	"net"
	"net/http"

	"golang.org/x/net/websocket"
)

// mockWSServer simulates the console subresource endpoint.
// It accepts WebSocket connections and echoes data back with a configurable prefix.
type mockWSServer struct {
	listener   net.Listener
	server     *http.Server
	echoPrefix string
	banner     string
}

// newMockWSServer creates and starts a mock WebSocket server.
// It listens on a random local port.
func newMockWSServer() (*mockWSServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	m := &mockWSServer{
		listener:   listener,
		echoPrefix: "echo: ",
		banner:     "Welcome to mock console\r\n",
	}

	mux := http.NewServeMux()
	mux.Handle("/apis/console.osac.openshift.io/v1alpha1/namespaces/", websocket.Handler(m.handleConsole))

	m.server = &http.Server{Handler: mux}
	go m.server.Serve(listener)

	return m, nil
}

// newMockWSServerWithHandler creates a mock WebSocket server with a custom handler.
func newMockWSServerWithHandler(handler func(ws *websocket.Conn)) (*mockWSServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	m := &mockWSServer{listener: listener}

	mux := http.NewServeMux()
	mux.Handle("/apis/console.osac.openshift.io/v1alpha1/namespaces/", websocket.Handler(handler))

	m.server = &http.Server{Handler: mux}
	go m.server.Serve(listener)

	return m, nil
}

// Addr returns the server's listener address.
func (m *mockWSServer) Addr() string {
	return m.listener.Addr().String()
}

// Close shuts down the mock server.
func (m *mockWSServer) Close() error {
	return m.server.Close()
}

// handleConsole handles WebSocket connections to the console subresource.
// It sends a banner, then echoes all received data back with a prefix.
func (m *mockWSServer) handleConsole(ws *websocket.Conn) {
	ws.PayloadType = websocket.BinaryFrame

	// Send banner.
	if m.banner != "" {
		ws.Write([]byte(m.banner))
	}

	// Echo loop.
	buf := make([]byte, 4096)
	for {
		n, err := ws.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			response := m.echoPrefix + string(buf[:n])
			_, err = ws.Write([]byte(response))
			if err != nil {
				return
			}
		}
	}
}
