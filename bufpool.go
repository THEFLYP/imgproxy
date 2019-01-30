package main

import (
	"bytes"
	"sort"
	"sync"
)

type intSlice []int

func (p intSlice) Len() int           { return len(p) }
func (p intSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p intSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

type bufPool struct {
	mutex   sync.Mutex
	name    string
	size    int
	buffers []*bytes.Buffer

	calls   intSlice
	callInd int

	throughput int
}

func newBufPool(name string, n int, size int) *bufPool {
	pool := bufPool{
		name:    name,
		size:    size,
		buffers: make([]*bytes.Buffer, n),
		calls:   make(intSlice, 1024),
	}

	for i := range pool.buffers {
		pool.buffers[i] = pool.new()
	}

	if pool.size > 0 {
		// Get the real cap
		pool.size = pool.buffers[0].Cap()
	}

	return &pool
}

func (p *bufPool) new() *bytes.Buffer {
	var buf *bytes.Buffer

	buf = new(bytes.Buffer)

	if p.size > 0 {
		buf.Grow(p.size)
	}

	return buf
}

func (p *bufPool) calibrateAndClean() {
	sort.Sort(p.calls)

	pos := int(float64(len(p.calls)) * 0.95)
	score := p.calls[pos]

	p.callInd = 0
	p.throughput = 64

	for {
		if p.throughput > score {
			break
		}
		p.throughput <<= 1
	}

	for i, buf := range p.buffers {
		if buf != nil && buf.Cap() > p.throughput {
			p.buffers[i] = nil
		}
	}
}

func (p *bufPool) Get(size int) *bytes.Buffer {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	minSize, maxSize, minInd, maxInd := -1, -1, -1, -1

	for i := 0; i < len(p.buffers); i++ {
		if p.buffers[i] != nil {
			cap := p.buffers[i].Cap()

			if size > 0 && cap >= size && (minSize > cap || minSize == -1) {
				minSize = cap
				minInd = i
			}

			if cap > maxSize {
				maxSize = cap
				maxInd = i
			}
		}
	}

	var buf *bytes.Buffer

	if minInd >= 0 {
		// We found buffer with the desired size
		buf = p.buffers[minInd]
		p.buffers[minInd] = nil
	} else if maxInd >= 0 {
		// We didn't find buffer with the desired size
		buf = p.buffers[maxInd]
		p.buffers[maxInd] = nil
	} else {
		// We didn't find buffers at all
		return p.new()
	}

	buf.Reset()

	return buf
}

func (p *bufPool) Put(buf *bytes.Buffer) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if buf.Len() > 0 {
		p.calls[p.callInd] = buf.Len()
		p.callInd++

		if p.callInd == len(p.calls) {
			p.calibrateAndClean()
		}
	}

	if p.throughput > 0 && buf.Cap() > p.throughput {
		return
	}

	for i, b := range p.buffers {
		if b == nil {
			p.buffers[i] = buf

			if prometheusEnabled && buf.Cap() > 0 {
				observeBufferSize(p.name, buf.Cap())
			}

			return
		}
	}
}
