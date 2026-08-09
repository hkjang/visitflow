package app

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) publishLobbyEvent(event string) {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	for ch := range s.events {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Server) lobbyStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unsupported", "실시간 스트림을 지원하지 않는 연결입니다")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan string, 8)
	s.eventsMu.Lock()
	s.events[ch] = struct{}{}
	s.eventsMu.Unlock()
	defer func() {
		s.eventsMu.Lock()
		delete(s.events, ch)
		s.eventsMu.Unlock()
		close(ch)
	}()
	_, _ = fmt.Fprintf(w, "event: ready\ndata: connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			_, _ = fmt.Fprintf(w, "event: visitflow\ndata: %s\n\n", event)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
