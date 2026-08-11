// Package concerns mirrors Illuminate\View\Compilers\Concerns.
//
// The files it answers to, in the clone at
// laravel_illuminate/view/Compilers/Concerns:
//
//	CompilesAuthorizations.php
//	CompilesClasses.php
//	CompilesComments.php
//	CompilesComponents.php
//	CompilesConditionals.php
//	CompilesEchos.php
//	CompilesErrors.php
//	CompilesFragments.php
//	CompilesHelpers.php
//	CompilesIncludes.php
//	CompilesInjections.php
//	CompilesJs.php
//	CompilesJson.php
//	CompilesLayouts.php
//	CompilesLoops.php
//	CompilesRawPhp.php
//	CompilesSessions.php
//	CompilesStacks.php
//	CompilesStyles.php
//	CompilesTranslations.php
//	CompilesUseStatements.php
//
// # Kyse directive cross-reference
//
// The kyse compiler (aru/internal/kyse) translates .kyse.go into Go at build
// time. It already handles the directives below. Those marked MISSING are
// listed with what the compiler needs to emit and what the view runtime
// already supports.
//
// ## Already implemented in kyse
//
//	@extends     → RenderInto(layout, data, sections)
//	@yield       → Yield(w, sections, name)
//	@include     → Include(w, name, data)
//	@csrf        → CSRF(w, data)
//	@if/@else/@elseif/@endif → compile-time Go if/else
//	@foreach/@for/@while     → compile-time Go for/range
//	{{ }}        → Text(v)
//	{!! !!}      → raw write
//	@php         → raw Go block
//	@section/@stop/@show → compiler emits section capture
//	@parent      → parentPlaceholder replacement
//	@verbatim    → raw passthrough
//
// ## MISSING directives (kyse does not emit these yet)
//
//	@push / @stack / @prepend   Factory.StartPush / YieldPushContent
//	@class                      html template or Tailwind merge
//	@json                       json.Marshal + html/template escaping
//	@once / @endonce            Factory.HasRenderedOnce / MarkAsRenderedOnce
//	@error / @enderror          Factory.YieldContent("errors") + field check
//	@auth / @guest / @endauth   auth.Gate check at compile time
//	@can / @cannot / @canany    auth.Gate.Policy check
//	@env / @production          build-time config check
//	@hasSection / @sectionMissing Factory.HasSection / SectionMissing
//	@checked / @disabled / @selected / @required / @readonly
//	                            conditional attribute output
//	@pushIf / @elsePushIf / @elsePush / @endPushIf
//	                            conditional push logic
//	@style                      Tailwind inline style helper
//	@lang / @choice             Factory.RenderTranslation
//	@inject                     no Go equivalent (DI is not runtime)
//	@js                         JSON.stringify for JavaScript embedding
//	@use                        Go import statement (compile time)
//	@dd / @dump                 debug helpers
//	@method                     HTTP method spoofing
//	@vite                       N/A (no Vite, RULE 13)
//	@fragment / @endfragment    Factory.StartFragment / StopFragment
//	@each                       Factory.RenderEach
//	@includeIf / @includeWhen / @includeUnless / @includeFirst
//	                            conditional includes
//	@component / @endcomponent  component rendering stack
//	@slot / @endslot            Factory.Slot / EndSlot
//	@props / @aware             component prop extraction
//	@forelse / @empty / @endforelse
//	                            foreach with empty fallback
//	@break / @continue          loop control
//	@session / @endsession      session value access in view
//	{{-- --}}                   comments (compile-time strip)
//	{{{ }}}                     triple-escaped echo
//	@extendsFirst               First layout that exists
//	@append / @overwrite        section append/overwrite
//	@classComponentOpening / @endComponentClass
//	                            class-based components (kyse handles these statically)
//
// The runtime methods these need (StartSection, StopSection, YieldContent,
// StartPush, YieldPushContent, StartFragment, AddLoop, etc.) are all
// implemented on view.Factory in the parent package.
//
// The kyse compiler is in aru/internal/kyse. It is not in this package and
// this package does not import it. The directive list above is the
// specification for what the compiler must emit; the runtime in view.Factory
// is what it emits into.
package concerns
