package signature

import (
	"container/list"
	"sync"
	"time"
)

type lruItem struct {
	key      string
	toolCall string
	entry    Entry
}

type LRU struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	byKey    map[string]*list.Element
	byToolID map[string]*list.Element
}

func NewLRU(capacity int) *LRU {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRU{
		capacity: capacity,
		ll:       list.New(),
		byKey:    make(map[string]*list.Element, capacity),
		byToolID: make(map[string]*list.Element, capacity),
	}
}

func (c *LRU) Put(e Entry) {
	key := e.Key()
	if key == "" || e.ToolCallID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.byKey[key]; ok {
		it := el.Value.(*lruItem)
		it.entry = e
		c.ll.MoveToFront(el)
		c.byToolID[e.ToolCallID] = el
		return
	}

	item := &lruItem{key: key, toolCall: e.ToolCallID, entry: e}
	el := c.ll.PushFront(item)
	c.byKey[key] = el
	c.byToolID[e.ToolCallID] = el

	for c.ll.Len() > c.capacity {
		back := c.ll.Back()
		if back == nil {
			break
		}
		old := back.Value.(*lruItem)
		delete(c.byKey, old.key)
		if old.toolCall != "" {
			delete(c.byToolID, old.toolCall)
		}
		c.ll.Remove(back)
	}
}

func (c *LRU) Get(requestID, toolCallID string) (Entry, bool) {
	if requestID == "" || toolCallID == "" {
		return Entry{}, false
	}
	key := requestID + ":" + toolCallID

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.byKey[key]
	if !ok {
		return Entry{}, false
	}
	it := el.Value.(*lruItem)
	it.entry.LastAccess = time.Now()
	c.ll.MoveToFront(el)
	return it.entry, true
}

func (c *LRU) GetByToolCallID(toolCallID string) (Entry, bool) {
	if toolCallID == "" {
		return Entry{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.byToolID[toolCallID]
	if !ok {
		return Entry{}, false
	}
	it := el.Value.(*lruItem)
	it.entry.LastAccess = time.Now()
	c.ll.MoveToFront(el)
	return it.entry, true
}

// Session-based lookup removed; signatures are indexed by tool_call_id only.
