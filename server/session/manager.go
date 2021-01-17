package session

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// stolen from https://astaxie.gitbooks.io/build-web-application-with-golang/content/en/06.2.html

type Session interface {
	Set(key, val interface{}) error
	Get(key interface{}) interface{}
	Delete(key interface{}) error
	ID() string
}

type Provider interface {
	SessionInit(id string) (Session, error)
	SessionRead(id string) (Session, error)
	SessionDestroy(id string) error
	SessionGC(maxLifeTime time.Duration)
}

type Manager struct {
	cookieName  string
	lock        sync.Mutex
	provider    Provider
	maxlifetime time.Duration
}

var provides = make(map[string]Provider)

func NewManager(provideName, cookieName string, maxlifetime time.Duration) (*Manager, error) {
	provider, ok := provides[provideName]
	if !ok {
		return nil, fmt.Errorf("session: unknown provider %q (forgotten import?)", provideName)
	}
	return &Manager{provider: provider, cookieName: cookieName, maxlifetime: maxlifetime}, nil
}

func (m *Manager) sessionID() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(b)
}

func (m *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session Session) {
	m.lock.Lock()
	defer m.lock.Unlock()
	cookie, err := r.Cookie(m.cookieName)
	if err != nil || cookie.Value == "" {
		sid := m.sessionID()
		session, _ = m.provider.SessionInit(sid)
		cookie := http.Cookie{
			Name:     m.cookieName,
			Value:    url.QueryEscape(sid),
			Path:     "/",
			Secure:   os.Getenv("name") == "prod",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(m.maxlifetime),
		}
		http.SetCookie(w, &cookie)
	} else {
		sid, _ := url.QueryUnescape(cookie.Value)
		session, _ = m.provider.SessionRead(sid)
	}
	return
}

func (m *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.cookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	sid, _ := url.QueryUnescape(cookie.Value)
	m.provider.SessionDestroy(sid)
	c := http.Cookie{
		Name:    m.cookieName,
		Path:    "/",
		Expires: time.Now(),
		MaxAge:  -1,
	}
	http.SetCookie(w, &c)
}

func (m *Manager) GC() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.provider.SessionGC(m.maxlifetime)
	time.AfterFunc(m.maxlifetime, func() {
		m.GC()
	})
}

// Register makes a session provider available by the provided name.
// If a Register is called twice with the same name or if the driver is nil,
// it panics.
func Register(name string, provider Provider) {
	if provider == nil {
		panic("session: Register provider is nil")
	}
	if _, dup := provides[name]; dup {
		panic("session: Register called twice for provider " + name)
	}
	provides[name] = provider
}
