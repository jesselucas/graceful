package graceful

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func ExampleServer_withContext() {
	// Setup context to stop server in .5 second
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Create Option function to set server handler
	setHandler := func(s *Server) {
		s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello, client")
		})
	}

	// Create Option function to set server context
	setContext := func(s *Server) {
		s.Context = ctx
	}

	go func() {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				fmt.Println(err)
			}

			return
		}
	}()

	// Create server and start serving
	s := NewServer(":1234", setHandler, setContext)
	s.ListenAndServe()
}
