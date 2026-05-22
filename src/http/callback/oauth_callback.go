package callback

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type OAuthCallbackServer struct {
	server *http.Server
	ln     net.Listener
	codeCh chan string
	errCh  chan error
}

func StartOAuthCallbackServer(host string, port int, expectedState string) (*OAuthCallbackServer, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 1455
	}

	cb := &OAuthCallbackServer{
		codeCh: make(chan string, 1),
		errCh:  make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")

		if expectedState == "" || state != expectedState {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h2>State mismatch.</h2></body></html>"))
			select {
			case cb.errCh <- fmt.Errorf("oauth state mismatch"):
			default:
			}
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<html><body><h2>Missing authorization code.</h2></body></html>"))
			select {
			case cb.errCh <- fmt.Errorf("missing authorization code"):
			default:
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h2>OpenAI authentication complete. You can close this tab.</h2></body></html>"))
		select {
		case cb.codeCh <- code:
		default:
		}
	})

	cb.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	cb.ln = ln

	go func() {
		_ = cb.server.Serve(ln)
	}()

	return cb, nil
}

func (c *OAuthCallbackServer) WaitForCode(ctx context.Context) (string, error) {
	select {
	case code := <-c.codeCh:
		return code, nil
	case err := <-c.errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *OAuthCallbackServer) Close() error {
	if c == nil || c.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.server.Shutdown(ctx)
}
