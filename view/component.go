package view

import (
	"fmt"
	"iter"
	"sync"
)

// Component is Illuminate\View\Component.
//
// In PHP it is an abstract class whose one abstract method is render. Go has no
// abstract class, so the contract is an interface and the shared state is
// BaseComponent, which a component embeds. The split is mechanical: everything
// PHP declared abstract is here, everything PHP implemented is there.
type Component interface {
	// Render is Component::render. It returns the view the component draws:
	// a *View, something with a ToHTML method, a func(map[string]any) any, or
	// the name of a registered view.
	Render() any
}

// BaseComponent is the implemented half of Illuminate\View\Component.
//
// A component embeds it to get the attribute bag, the alias name and the
// methods the compiler-generated code calls around a render.
type BaseComponent struct {
	// ComponentName is the alias the component was written as.
	ComponentName string

	// Attributes is the bag of everything the caller wrote that the component
	// did not declare.
	Attributes *ComponentAttributeBag

	// Props are the names the component declares. They are what
	// IgnoredParameterNames answers with, and they stand in for the PHP
	// constructor parameters that reflection reads there.
	Props []string
}

// componentState holds what PHP keeps in Component's static properties.
var componentState struct {
	mu        sync.RWMutex
	factory   *Factory
	resolver  func(name string, data map[string]any) Component
	viewCache map[string]string
}

// Data is Component::data.
//
// PHP reflects over the component's public properties and methods to build the
// data. Go rejects that mechanism (it is the one the framework exists to
// replace), so a component states its data instead: the attribute bag, plus
// whatever the embedding type passes in. The two extraction helpers PHP needs
// for the reflection -- extractPublicProperties and extractPublicMethods -- are
// protected there and have no counterpart here.
func (c *BaseComponent) Data() map[string]any {
	if c.Attributes == nil {
		c.Attributes = c.newAttributeBag(nil)
	}
	data := c.Attributes.All()
	data["attributes"] = c.Attributes
	return data
}

// WithName is Component::withName.
func (c *BaseComponent) WithName(name string) *BaseComponent {
	c.ComponentName = name
	return c
}

// WithAttributes is Component::withAttributes.
func (c *BaseComponent) WithAttributes(attributes map[string]any) *BaseComponent {
	if c.Attributes == nil {
		c.Attributes = c.newAttributeBag(nil)
	}
	c.Attributes.SetAttributes(attributes)
	return c
}

// newAttributeBag is Component::newAttributeBag.
func (c *BaseComponent) newAttributeBag(attributes map[string]any) *ComponentAttributeBag {
	return NewComponentAttributeBag(attributes)
}

// ShouldRender is Component::shouldRender.
//
// A component that embeds BaseComponent and wants to disappear under some
// condition shadows this method, the way a PHP subclass overrides it.
func (c *BaseComponent) ShouldRender() bool { return true }

// IgnoredParameterNames is Component::ignoredParameterNames.
//
// PHP reads the constructor parameter names off the class with reflection. The
// Go counterpart is Props, which the component states.
func (c *BaseComponent) IgnoredParameterNames() []string {
	out := make([]string, len(c.Props))
	copy(out, c.Props)
	return out
}

// View is Component::view.
//
// It draws a view through the factory the component is attached to, the same
// one ForgetFactory drops.
func (c *BaseComponent) View(view string, data map[string]any) *View {
	return componentFactory().Make(view, data)
}

// ResolveView is Component::resolveView.
//
// PHP calls $this->render() on itself; Go has no abstract-class self-dispatch,
// so the component is the argument. A *View or anything with a ToHTML method is
// returned as it is; a func is wrapped so it resolves when the data arrives;
// a string is the name of a registered view.
//
// The PHP pair that writes an inline template to disk and registers it under
// the __components namespace -- extractBladeViewFromString and
// createBladeViewFromString -- is protected and has no counterpart: a view here
// is compiled into the binary, never written at request time.
func ResolveView(c Component) any {
	rendered := c.Render()

	switch value := rendered.(type) {
	case *View:
		return value
	case interface{ ToHTML() string }:
		return value
	case func(map[string]any) any:
		return func(data map[string]any) any {
			resolved := value(data)
			if v, ok := resolved.(*View); ok {
				return v
			}
			return resolved
		}
	default:
		return rendered
	}
}

// Resolve is Component::resolve.
//
// The resolver registered by ResolveComponentsUsing gets first refusal. PHP
// falls back to the container when the data does not cover every constructor
// parameter; there is no container here (ADR 0001), so the fallback is an error
// naming the component.
func Resolve(name string, data map[string]any) (Component, error) {
	componentState.mu.RLock()
	resolver := componentState.resolver
	componentState.mu.RUnlock()

	if resolver == nil {
		return nil, fmt.Errorf("view: no resolver can build the component %q. Call view.ResolveComponentsUsing first", name)
	}

	component := resolver(name, data)
	if component == nil {
		return nil, fmt.Errorf("view: the component resolver returned nothing for %q", name)
	}
	return component, nil
}

// ResolveComponentsUsing is Component::resolveComponentsUsing.
func ResolveComponentsUsing(resolver func(name string, data map[string]any) Component) {
	componentState.mu.Lock()
	componentState.resolver = resolver
	componentState.mu.Unlock()
}

// ForgetComponentsResolver is Component::forgetComponentsResolver.
func ForgetComponentsResolver() {
	componentState.mu.Lock()
	componentState.resolver = nil
	componentState.mu.Unlock()
}

// ForgetFactory is Component::forgetFactory.
func ForgetFactory() {
	componentState.mu.Lock()
	componentState.factory = nil
	componentState.mu.Unlock()
}

// FlushCache is Component::flushCache.
//
// PHP clears four static caches; three of them hold reflection results that do
// not exist here, so what is left is the inline-view cache.
func FlushCache() {
	componentState.mu.Lock()
	componentState.viewCache = map[string]string{}
	componentState.mu.Unlock()
}

// componentFactory is Component::factory. It builds one on first use, because
// there is no container to resolve it from.
func componentFactory() *Factory {
	componentState.mu.Lock()
	defer componentState.mu.Unlock()
	if componentState.factory == nil {
		componentState.factory = NewFactory()
	}
	return componentState.factory
}

// AnonymousComponent is Illuminate\View\AnonymousComponent.
//
// It is a component with no behaviour: a view name and the data to draw it
// with, which is what a component written as markup alone compiles to.
type AnonymousComponent struct {
	BaseComponent

	view string
	data map[string]any
}

// NewAnonymousComponent is AnonymousComponent::__construct.
func NewAnonymousComponent(view string, data map[string]any) *AnonymousComponent {
	copied := make(map[string]any, len(data))
	for k, v := range data {
		copied[k] = v
	}
	return &AnonymousComponent{view: view, data: copied}
}

// Render is AnonymousComponent::render.
func (c *AnonymousComponent) Render() any { return c.view }

// Data is AnonymousComponent::data.
//
// The parent bag comes first, then this component's own attributes, then the
// data it was built with, and the bag itself last under "attributes" -- the
// order is what decides which value wins.
func (c *AnonymousComponent) Data() map[string]any {
	if c.Attributes == nil {
		c.Attributes = c.newAttributeBag(nil)
	}

	data := map[string]any{}
	if parent, ok := c.data["attributes"].(*ComponentAttributeBag); ok {
		for k, v := range parent.GetAttributes() {
			data[k] = v
		}
	}
	for k, v := range c.Attributes.GetAttributes() {
		data[k] = v
	}
	for k, v := range c.data {
		data[k] = v
	}
	data["attributes"] = c.Attributes
	return data
}

// DynamicComponent is Illuminate\View\DynamicComponent.
//
// It draws the component whose name is only known at render time.
//
// PHP builds a Blade template that writes an <x-...> tag and compiles it again;
// there are no component tags here (RULE 13), so what it resolves to is the
// name of a registered view, which is the same answer without the second pass.
// The four protected helpers that exist only to write that tag -- compileProps,
// compileBindings, compileSlots and classForComponent -- have no counterpart.
type DynamicComponent struct {
	BaseComponent

	// Component is the name of the component to draw.
	Component string
}

// NewDynamicComponent is DynamicComponent::__construct.
func NewDynamicComponent(component string) *DynamicComponent {
	return &DynamicComponent{Component: component}
}

// Render is DynamicComponent::render.
func (c *DynamicComponent) Render() any { return c.Component }

// InvokableComponentVariable is Illuminate\View\InvokableComponentVariable.
//
// It is a component method exposed to the view as a value: the view writes the
// name and the call happens when the value is drawn, not before.
type InvokableComponentVariable struct {
	callable func() any
}

// NewInvokableComponentVariable is InvokableComponentVariable::__construct.
func NewInvokableComponentVariable(callable func() any) *InvokableComponentVariable {
	return &InvokableComponentVariable{callable: callable}
}

// ResolveDisplayableValue is InvokableComponentVariable::resolveDisplayableValue.
func (v *InvokableComponentVariable) ResolveDisplayableValue() any { return v.Invoke() }

// Invoke is InvokableComponentVariable::__invoke.
func (v *InvokableComponentVariable) Invoke() any {
	if v.callable == nil {
		return nil
	}
	return v.callable()
}

// GetIterator is InvokableComponentVariable::getIterator.
//
// PHP returns an ArrayIterator over the resolved value; Go returns a
// range-over-func sequence, and a value that is not a slice yields once.
func (v *InvokableComponentVariable) GetIterator() iter.Seq2[int, any] {
	return func(yield func(int, any) bool) {
		switch resolved := v.Invoke().(type) {
		case nil:
			return
		case []any:
			for i, item := range resolved {
				if !yield(i, item) {
					return
				}
			}
		default:
			yield(0, resolved)
		}
	}
}

// String is InvokableComponentVariable::__toString.
func (v *InvokableComponentVariable) String() string { return Text(v.Invoke()) }
