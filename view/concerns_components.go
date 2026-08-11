package view

// ManagesComponents mirrors Illuminate\View\Concerns\ManagesComponents.
//
// These are methods on Factory. Component rendering is handled by the kyse
// compiler at build time, so the runtime methods here are for state tracking
// and the component stack — they support the kyse-generated code, not a
// runtime component discovery mechanism.

// ComponentSlot holds the content and attributes of a component slot.
type ComponentSlot struct {
	Content    string
	Attributes map[string]string
}

// StartComponent pushes a component onto the rendering stack.
func (f *Factory) StartComponent(name string, data map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.componentStack = append(f.componentStack, name)
	f.currentComponent = name
	f.currentComponentData = data
	if f.slots == nil {
		f.slots = map[string]ComponentSlot{}
	}
}

// StartComponentFirst tries each name in order and uses the first that exists.
func (f *Factory) StartComponentFirst(names []string, data map[string]any) {
	for _, name := range names {
		if f.Exists(name) {
			f.StartComponent(name, data)
			return
		}
	}
}

// RenderComponent pops the current component from the stack.
func (f *Factory) RenderComponent() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.componentStack) == 0 {
		return ""
	}

	last := len(f.componentStack) - 1
	f.componentStack = f.componentStack[:last]

	if len(f.componentStack) > 0 {
		f.currentComponent = f.componentStack[len(f.componentStack)-1]
	} else {
		f.currentComponent = ""
	}

	return f.componentBuffer
}

// Slot begins capturing a named slot for the current component.
func (f *Factory) Slot(name string, content string, attributes map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if content != "" {
		f.slots[name] = ComponentSlot{Content: content, Attributes: attributes}
		return
	}
	f.slotStack = append(f.slotStack, name)
}

// EndSlot closes the current slot capture.
func (f *Factory) EndSlot() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.slotStack) == 0 {
		return
	}

	last := len(f.slotStack) - 1
	name := f.slotStack[last]
	f.slotStack = f.slotStack[:last]

	f.slots[name] = ComponentSlot{Content: f.slotBuffer}
	f.slotBuffer = ""
}

// GetConsumableComponentData walks up the component tree looking for a key.
func (f *Factory) GetConsumableComponentData(key string, fallback any) any {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Start from the current component data and walk up.
	// In a simple implementation, we check currentComponentData first,
	// then any parent component data stacks.
	if f.currentComponentData != nil {
		if val, ok := f.currentComponentData[key]; ok {
			return val
		}
	}
	if f.componentData != nil {
		if val, ok := f.componentData[key]; ok {
			return val
		}
	}
	return fallback
}

// FlushComponents resets all component state.
func (f *Factory) FlushComponents() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.componentStack = nil
	f.componentData = nil
	f.currentComponentData = nil
	f.currentComponent = ""
	f.slots = map[string]ComponentSlot{}
	f.slotStack = nil
	f.componentBuffer = ""
	f.slotBuffer = ""
}

// SetComponentBuffer sets the output buffer for a component render.
func (f *Factory) SetComponentBuffer(content string) {
	f.mu.Lock()
	f.componentBuffer = content
	f.mu.Unlock()
}

// SetSlotBuffer sets the output buffer for a slot capture.
func (f *Factory) SetSlotBuffer(content string) {
	f.mu.Lock()
	f.slotBuffer = content
	f.mu.Unlock()
}

// NewComponentHash generates a hash for the component stack.
func (f *Factory) NewComponentHash(name string) string {
	// Simple deterministic hash — the kyse compiler generates these.
	return name + "-" + itoa(len(f.componentStack))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
