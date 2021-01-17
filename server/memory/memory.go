package memory

import (
	"bison-bakeshop/session"
	"container/list"
	"sync"
	"time"
)

type Provider struct {
	lock     sync.Mutex
	sessions map[string]*list.Element
	list     *list.List
}

var prov = &Provider{list: list.New()}

type Store struct {
	sid          string
	timeAccessed time.Time
	value        map[interface{}]interface{}
}

func (s *Store) Set(key, value interface{}) error {
	s.value[key] = value
	return prov.SessionUpdate(s.sid)
}

func (s *Store) Get(key interface{}) interface{} {
	prov.SessionUpdate(s.sid)
	if v, ok := s.value[key]; ok {
		return v
	}
	return nil
}

func (s *Store) Delete(key interface{}) error {
	delete(s.value, key)
	return prov.SessionUpdate(s.sid)
}

func (s *Store) ID() string {
	return s.sid
}

func (p *Provider) SessionInit(id string) (session.Session, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	v := make(map[interface{}]interface{}, 0)
	sess := &Store{
		sid:          id,
		timeAccessed: time.Now(),
		value:        v,
	}
	element := p.list.PushBack(sess)
	p.sessions[id] = element
	return sess, nil
}

func (p *Provider) SessionRead(id string) (session.Session, error) {
	if el, ok := p.sessions[id]; ok {
		return el.Value.(*Store), nil
	}
	return p.SessionInit(id)
}

func (p *Provider) SessionDestroy(id string) error {
	if el, ok := p.sessions[id]; ok {
		delete(p.sessions, id)
		p.list.Remove(el)
	}
	return nil
}

func (p *Provider) SessionGC(maxLifeTime time.Duration) {
	p.lock.Lock()
	defer p.lock.Unlock()
	for {
		el := p.list.Back()
		if el == nil {
			break
		}
		if el.Value.(*Store).timeAccessed.Add(maxLifeTime).Unix() < time.Now().Unix() {
			p.list.Remove(el)
			delete(p.sessions, el.Value.(*Store).sid)
		} else {
			break
		}
	}
}

func (p *Provider) SessionUpdate(sid string) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if el, ok := p.sessions[sid]; ok {
		el.Value.(*Store).timeAccessed = time.Now()
		p.list.MoveToFront(el)
	}
	return nil
}

// ew. Is there a nicer way?
func init() {
	prov.sessions = make(map[string]*list.Element, 0)
	session.Register("memory", prov)
}
