package signature

import (
	"container/list"
	"sync"
	"time"
)

type lruItem struct {
	key      string
	toolCall string
	index    EntryIndex
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

func (c *LRU) Put(idx EntryIndex) {
	key := idx.Key()
	if key == "" || idx.ToolCallID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.byKey[key]; ok {
		it := el.Value.(*lruItem)
		it.index = idx
		c.ll.MoveToFront(el)
		c.byToolID[idx.ToolCallID] = el
		return
	}

	item := &lruItem{key: key, toolCall: idx.ToolCallID, index: idx}
	el := c.ll.PushFront(item)
	c.byKey[key] = el
	c.byToolID[idx.ToolCallID] = el

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

func (c *LRU) Get(requestID, toolCallID string) (EntryIndex, bool) {
	if requestID == "" || toolCallID == "" {
		return EntryIndex{}, false
	}
	key := requestID + ":" + toolCallID

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.byKey[key]
	if !ok {
		return EntryIndex{}, false
	}
	it := el.Value.(*lruItem)
	it.index.LastAccess = time.Now()
	c.ll.MoveToFront(el)
	return it.index, true
}

func (c *LRU) GetByToolCallID(toolCallID string) (EntryIndex, bool) {
	if toolCallID == "" {
		return EntryIndex{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.byToolID[toolCallID]
	if !ok {
		return EntryIndex{}, false
	}
	it := el.Value.(*lruItem)
	it.index.LastAccess = time.Now()
	c.ll.MoveToFront(el)
	return it.index, true
}
