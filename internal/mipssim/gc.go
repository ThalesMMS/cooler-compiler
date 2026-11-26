// SPDX-License-Identifier: AGPL-3.0-or-later

package mipssim

import "sort"

// memoryManager allocates Cool objects on the simulated heap. It honors the
// program's memory-manager selection:
//
//   - NoGC (_NoGC_Init): bump allocation, never collects;
//   - GenGC (_GenGC_Init, coolc -g): a non-moving mark-sweep collector that
//     runs when the heap reaches a threshold, records _GenGC_Assign write
//     barriers, and reuses freed blocks of the same size;
//   - _MemMgr_TEST = 1 (coolc -t): collect before every allocation.
//
// Roots are every register and every word between $sp and the top of the
// stack, the places the Cool runtime's collectors scan. Marking is precise
// inside objects (Int and Bool hold raw values, String holds one pointer)
// and conservative for roots: a root word keeps an object alive when it is
// exactly that object's address. Freed blocks are poisoned so a dangling
// pointer fails loudly, and -T header checks (_gc_check) verify the -1
// eyecatcher in front of every stored object.
type memoryManager struct {
	m            *machine
	stats        GCStats
	objects      map[uint32]int // object address -> size in words
	free         map[int][]uint32
	basicTags    map[string]int32
	pinned       map[uint32]int
	nextCollect  int
	collectEvery bool
	generational bool
	remembered   map[uint32]bool
}

const (
	gcInitialThreshold = 256 << 10
	poison             = 0x0BADF00D
)

func newMemoryManager(m *machine, collector string, test bool) *memoryManager {
	gc := &memoryManager{
		m: m, objects: map[uint32]int{}, free: map[int][]uint32{}, basicTags: map[string]int32{},
		pinned: map[uint32]int{}, nextCollect: gcInitialThreshold, collectEvery: test,
		generational: collector == "GenGC", remembered: map[uint32]bool{},
	}
	gc.stats.Collector = collector
	gc.stats.CollectEvery = test
	return gc
}

func (gc *memoryManager) pin(object uint32)   { gc.pinned[object]++ }
func (gc *memoryManager) unpin(object uint32) { gc.pinned[object]-- }

// allocate returns the address of a fresh object of words words (header
// included), preceded by the -1 eyecatcher.
func (gc *memoryManager) allocate(words int) (uint32, error) {
	m := gc.m
	gc.stats.Allocations++
	if gc.collectEvery || (gc.generational && gc.stats.HeapBytes >= gc.nextCollect) {
		if err := gc.collect(); err != nil {
			return 0, err
		}
	}
	if blocks := gc.free[words]; len(blocks) > 0 {
		object := blocks[len(blocks)-1]
		gc.free[words] = blocks[:len(blocks)-1]
		gc.objects[object] = words
		return object, m.storeWord(object-4, -1)
	}
	bytes := 4 * (words + 1)
	if len(m.memory)+bytes > m.options.Limits.MaxHeapBytes+len(m.program.data) {
		if gc.generational && !gc.collectEvery {
			if err := gc.collect(); err != nil {
				return 0, err
			}
			if blocks := gc.free[words]; len(blocks) > 0 {
				return gc.allocate(words)
			}
		}
		return 0, m.fail("heap-exhausted", "Execution stopped: the heap exceeded %d bytes.", m.options.Limits.MaxHeapBytes)
	}
	base := m.heapTop
	m.memory = append(m.memory, make([]byte, bytes)...)
	m.heapTop += uint32(bytes)
	gc.stats.HeapBytes += bytes
	object := base + 4
	gc.objects[object] = words
	return object, m.storeWord(base, -1)
}

// copyObject implements Object.copy.
func (gc *memoryManager) copyObject(source uint32) (uint32, error) {
	m := gc.m
	words, err := m.loadWord(source + objectSize)
	if err != nil {
		return 0, err
	}
	if words < 3 || int(words) > m.options.Limits.MaxHeapBytes/4 {
		return 0, m.fail("bad-object", "Exception: Object.copy of a corrupt object at 0x%08x.", source)
	}
	gc.pin(source)
	object, err := gc.allocate(int(words))
	gc.unpin(source)
	if err != nil {
		return 0, err
	}
	from, fromOffset, err := m.translate(source, 4*int(words))
	if err != nil {
		return 0, err
	}
	to, toOffset, err := m.translate(object, 4*int(words))
	if err != nil {
		return 0, err
	}
	copy(to[toOffset:toOffset+4*int(words)], from[fromOffset:fromOffset+4*int(words)])
	return object, nil
}

func (gc *memoryManager) recordAssignment(address uint32) {
	gc.stats.AssignmentLogs++
	if gc.generational {
		gc.remembered[address] = true
	}
}

// check implements _gc_check: a stored object must carry the eyecatcher.
func (gc *memoryManager) check(object uint32) error {
	gc.stats.HeaderChecks++
	if object == 0 {
		return nil
	}
	header, err := gc.m.loadWord(object - 4)
	if err != nil {
		return err
	}
	if header != -1 {
		return gc.m.fail("gc-check", "GC check failed: object 0x%08x has no -1 eyecatcher.", object)
	}
	return nil
}

func (gc *memoryManager) collect() error {
	m := gc.m
	gc.stats.Collections++
	marked := map[uint32]bool{}
	var stack []uint32
	consider := func(value int32) {
		address := uint32(value)
		if _, ok := gc.objects[address]; ok && !marked[address] {
			marked[address] = true
			stack = append(stack, address)
		}
	}
	for _, value := range m.reg {
		consider(value)
	}
	for address := range gc.pinned {
		if gc.pinned[address] > 0 {
			consider(int32(address))
		}
	}
	for address := uint32(m.reg[29]); address+4 <= StackTop+4 && address >= StackTop+4-uint32(len(m.stack)); address += 4 {
		value, err := m.loadWord(address &^ 3)
		if err != nil {
			return err
		}
		consider(value)
	}
	for len(stack) > 0 {
		object := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		tag, err := m.loadWord(object + objectTag)
		if err != nil {
			return err
		}
		words := gc.objects[object]
		switch tag {
		case gc.basicTags["_int_tag"], gc.basicTags["_bool_tag"]:
			continue
		case gc.basicTags["_string_tag"]:
			words = 4
		}
		for offset := firstAttr; offset < 4*words; offset += 4 {
			value, err := m.loadWord(object + uint32(offset))
			if err != nil {
				return err
			}
			consider(value)
		}
	}
	addresses := make([]uint32, 0, len(gc.objects))
	for address := range gc.objects {
		if !marked[address] {
			addresses = append(addresses, address)
		}
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })
	for _, address := range addresses {
		words := gc.objects[address]
		delete(gc.objects, address)
		for offset := -4; offset < 4*words; offset += 4 {
			if err := m.storeWord(address+uint32(offset), poison); err != nil {
				return err
			}
		}
		gc.free[words] = append(gc.free[words], address)
		gc.stats.FreedObjects++
	}
	gc.remembered = map[uint32]bool{}
	gc.nextCollect = 2*gc.liveBytes() + gcInitialThreshold
	return nil
}

func (gc *memoryManager) liveBytes() int {
	total := 0
	for _, words := range gc.objects {
		total += 4 * (words + 1)
	}
	return total
}
