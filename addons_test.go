package redcon

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenerAccept(t *testing.T) {
	port := 8000

	go func() {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%v", port))
		if err != nil {
			t.Errorf("listen failed: %v", err)
			return
		}
		defer listener.Close()

		mux := sync.Mutex{}
		conns := map[net.Conn]bool{}

		for {
			start := time.Now()
			c, err := listener.Accept()
			if err != nil {
				t.Errorf("Accept failed: %v", err)
				return
			}

			go func() {
				t.Logf("", time.Now().Sub(start))
			}()

			mux.Lock()
			conns[c] = true
			mux.Unlock()

			go testHandleAddons(c)
		}
	}()

	go func() {
		time.Sleep(time.Second * 3)

		wg := sync.WaitGroup{}

		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				//start := time.Now()
				c, err := net.Dial("tcp", fmt.Sprintf(":%v", port))
				defer func() {
					//t.Logf("dial time:(%d)\n", time.Now().Sub(start).Microseconds())
					_ = c.Close()
					wg.Done()
				}()
				if err != nil {
					t.Errorf("dial failed: %v", err)
				}
			}()
		}

		wg.Wait()
		t.Log("done")
	}()

	time.Sleep(time.Second * 10)
}

var (
	connCounter int64 = 0
)

func testHandleAddons(conn net.Conn) {
	atomic.AddInt64(&connCounter, -1)
	log.Println(conn.RemoteAddr())
}
