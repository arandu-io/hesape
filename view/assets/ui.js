/* Arandu client behaviours.
 *
 * Everything interactive on a page is bound once, on `document`, and dispatched
 * by looking at `data-*` attributes on the element an event came from. Nothing
 * here reads an attribute and evaluates it: attributes carry data, never code.
 *
 * That is the whole reason this file exists rather than Alpine. Alpine compiles
 * every directive expression with `new AsyncFunction`, and this framework serves
 * `script-src 'self'` with no `unsafe-eval`, so every directive threw at the
 * point of evaluation and no client behaviour ever ran. Loosening the policy
 * would buy the behaviour back at the price of the policy, and building the same
 * evaluator by hand would reopen the same hole under a different name.
 *
 * Because it is delegation, markup that HTMX swaps in is live the moment it
 * lands: every behaviour this file ships needs no initialising and no tearing
 * down. The one sweep below stamps state that only the browser knows -- which
 * accent is in force -- onto attributes a stylesheet and a screen reader can
 * see. Skipping the sweep costs a checkmark, never an interaction.
 *
 * Those behaviours read no expression, keep no parallel state, and store
 * nothing per element: open, active and selected all live in the ARIA the
 * markup already has to carry, so the DOM is the state and there is only one
 * copy of it.
 *
 * # The one thing that does have a lifecycle
 *
 * An application registers behaviours of its own by name -- arandu.ui.define
 * and arandu.ui.action, below -- and those get mounted, updated and destroyed,
 * because a behaviour somebody else wrote may take a timer or an observer and
 * has to be told when its element is going away. That is the exception, it is
 * hooked to htmx's own events, and it changes nothing about the rule: the
 * attribute holds a name that is looked up in a map, never code that is run.
 *
 * Loading this file twice is a no-op.
 */
(function () {
	'use strict';

	var arandu = window.arandu = window.arandu || {};
	if (arandu.ui) return;
	arandu.ui = { version: 1 };

	var OPTION = '[role="option"]';
	var LINE = '[role="menuitem"]';
	var COPY_RESET = 1600;

	var copyTimers = new WeakMap();

	/* The element an event came from, or null when it was not an element. */
	function origin(event) {
		var node = event.target;
		return node && node.nodeType === 1 ? node : null;
	}

	/* Whether an item can be reached: not disabled, and not filtered away. */
	function usable(item) {
		return !!item &&
			item.getAttribute('aria-disabled') !== 'true' &&
			item.getAttribute('aria-hidden') !== 'true' &&
			!item.hasAttribute('disabled');
	}

	function list(container, selector) {
		if (!container) return [];
		var found = container.querySelectorAll(selector);
		var out = [];
		for (var i = 0; i < found.length; i++) {
			if (usable(found[i])) out.push(found[i]);
		}
		return out;
	}

	/* ---- active item -------------------------------------------------------
	 *
	 * Which item is active is held in one place, aria-activedescendant on the
	 * text box, because that is the attribute a screen reader reads. The class
	 * on the item is what the stylesheet paints, and it is kept in step here so
	 * the two can never disagree.
	 */

	function activeID(input) {
		return input ? input.getAttribute('aria-activedescendant') : null;
	}

	function setActive(input, item, container, selector, scroll) {
		if (container) {
			var all = container.querySelectorAll(selector);
			for (var i = 0; i < all.length; i++) {
				if (all[i] !== item) all[i].classList.remove('active');
			}
		}
		if (!item) {
			if (input) input.removeAttribute('aria-activedescendant');
			return;
		}
		item.classList.add('active');
		if (input) {
			if (item.id) input.setAttribute('aria-activedescendant', item.id);
			else input.removeAttribute('aria-activedescendant');
		}
		if (scroll && item.scrollIntoView) item.scrollIntoView({ block: 'nearest' });
	}

	function move(input, container, selector, step) {
		var all = list(container, selector);
		if (!all.length) return;

		var current = activeID(input);
		var at = -1;
		for (var i = 0; i < all.length; i++) {
			if (all[i].id && all[i].id === current) { at = i; break; }
		}
		var to = at < 0 ? (step > 0 ? 0 : all.length - 1) : (at + step + all.length) % all.length;
		setActive(input, all[to], container, selector, true);
	}

	/* ---- theme -------------------------------------------------------------
	 *
	 * The state itself is theme.js's: it applies the choice to <html> before the
	 * body is parsed and owns what is written to storage. This only asks it to
	 * change, and reflects the answer onto the buttons.
	 */

	function theme() {
		return arandu.theme || null;
	}

	function setMode(mode) {
		var store = theme();
		if (!mode || !store || typeof store.set !== 'function') return;
		store.set(mode);
		stamp(document);
	}

	/* ---- copy --------------------------------------------------------------
	 *
	 * The text is read out of an attribute and handed to the clipboard as text.
	 * Which of the two words the button reads is the stylesheet's, keyed on the
	 * marker set here.
	 */

	function copy(button) {
		var text = button.getAttribute('data-copy-text');
		if (text === null || !navigator.clipboard) return;

		navigator.clipboard.writeText(text).then(function () {
			window.clearTimeout(copyTimers.get(button));
			button.setAttribute('data-copied', '');
			copyTimers.set(button, window.setTimeout(function () {
				button.removeAttribute('data-copied');
				copyTimers.delete(button);
			}, COPY_RESET));
		}, function () {
			/* No clipboard permission, or an insecure origin. The line is on the
			 * page and can be selected; saying nothing beats saying "copied" to
			 * somebody whose clipboard is empty. */
		});
	}

	/* ---- combobox ----------------------------------------------------------
	 *
	 * The list comes from the server on every keystroke, over HTMX, and is
	 * swapped into the listbox whole. Nothing here filters and nothing here
	 * fetches: this opens and closes the popover, walks the options that are
	 * present, and writes the chosen one into the two inputs.
	 */

	function comboboxParts(box) {
		return {
			input: box.querySelector('input[role="combobox"]'),
			listbox: box.querySelector('[role="listbox"]'),
			popover: box.querySelector('[data-popover]'),
			value: box.querySelector('[data-combobox-value]')
		};
	}

	function setComboboxOpen(box, open) {
		var parts = comboboxParts(box);
		if (parts.input) parts.input.setAttribute('aria-expanded', open ? 'true' : 'false');
		if (parts.popover) parts.popover.setAttribute('aria-hidden', open ? 'false' : 'true');
		if (!open) setActive(parts.input, null, parts.listbox, OPTION, false);
	}

	function comboboxOpen(box) {
		var input = box.querySelector('input[role="combobox"]');
		return !!input && input.getAttribute('aria-expanded') === 'true';
	}

	function chooseOption(box, option) {
		if (!usable(option)) return;
		var parts = comboboxParts(box);

		if (parts.listbox) {
			var all = parts.listbox.querySelectorAll(OPTION);
			for (var i = 0; i < all.length; i++) {
				if (all[i] === option) all[i].setAttribute('aria-selected', 'true');
				else all[i].removeAttribute('aria-selected');
			}
		}
		if (parts.value) parts.value.value = option.getAttribute('data-value') || '';
		if (parts.input) {
			var label = option.getAttribute('data-label');
			parts.input.value = label === null ? option.textContent.trim() : label;
		}
		setComboboxOpen(box, false);
	}

	function comboboxKey(event, box, input) {
		var parts = comboboxParts(box);

		if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
			event.preventDefault();
			setComboboxOpen(box, true);
			move(input, parts.listbox, OPTION, event.key === 'ArrowDown' ? 1 : -1);
			return;
		}
		if (event.key === 'Enter') {
			if (input.getAttribute('aria-expanded') !== 'true') return;
			event.preventDefault();
			var id = activeID(input);
			var option = id ? document.getElementById(id) : null;
			if (option && parts.listbox && parts.listbox.contains(option)) chooseOption(box, option);
			return;
		}
		if (event.key === 'Escape') setComboboxOpen(box, false);
	}

	/* ---- command palette ---------------------------------------------------
	 *
	 * Every line is in the document from the first paint and stays there. What
	 * is typed sets aria-hidden on the lines it does not match, and the
	 * stylesheet does the rest: it hides a hidden line, hides a group whose
	 * lines all went, and draws the empty message when none are left. Nothing is
	 * fetched, nothing is removed, and the search box's own value is the query
	 * -- there is no second copy of it to fall out of step.
	 */

	function commandParts(palette) {
		return {
			input: palette.querySelector('input[role="combobox"]'),
			menu: palette.querySelector('[role="menu"]')
		};
	}

	function filterCommand(palette) {
		var parts = commandParts(palette);
		if (!parts.menu) return;

		var query = parts.input ? parts.input.value.trim().toLowerCase() : '';
		var lines = parts.menu.querySelectorAll(LINE);
		for (var i = 0; i < lines.length; i++) {
			var line = lines[i];
			var haystack = (line.getAttribute('data-search') || line.textContent || '').toLowerCase();
			if (query && haystack.indexOf(query) === -1) line.setAttribute('aria-hidden', 'true');
			else line.removeAttribute('aria-hidden');
		}

		/* A line that was active and has just been filtered away is a line the
		 * Enter key would still follow. */
		var current = activeID(parts.input);
		var active = current ? document.getElementById(current) : null;
		if (current && (!active || !parts.menu.contains(active) || !usable(active))) {
			setActive(parts.input, null, parts.menu, LINE, false);
		}
	}

	function commandKey(event, palette, input) {
		var parts = commandParts(palette);

		if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
			event.preventDefault();
			move(input, parts.menu, LINE, event.key === 'ArrowDown' ? 1 : -1);
			return;
		}
		if (event.key === 'Enter') {
			event.preventDefault();
			var id = activeID(input);
			var line = id ? document.getElementById(id) : null;
			if (line && parts.menu && parts.menu.contains(line) && usable(line)) line.click();
			return;
		}
		if (event.key === 'Escape') {
			input.value = '';
			filterCommand(palette);
			setActive(input, null, parts.menu, LINE, false);
		}
	}

	/* ---- range slider ------------------------------------------------------
	 *
	 * The server draws the filled track and the number, so a slider is right
	 * before this runs and stays right if it never does. This keeps both in step
	 * with a thumb that is moving.
	 */

	function paintSlider(track) {
		var min = Number(track.min);
		var max = Number(track.max);
		var value = Number(track.value);
		if (!isFinite(min)) min = 0;
		if (!isFinite(max)) max = 100;
		if (!isFinite(value)) value = min;

		var span = max - min;
		var fill = span > 0 ? ((value - min) / span) * 100 : 0;
		track.style.setProperty('--slider-value', fill + '%');

		var field = track.closest('[data-slider]');
		var output = field ? field.querySelector('[data-slider-output]') : null;
		if (output) output.textContent = track.value;
	}

	/* ---- the sweep ---------------------------------------------------------
	 *
	 * Two things a server cannot write and delegation cannot reach, because they
	 * are state rather than events. Which accent is in force lives in the
	 * browser, so no cached page can carry aria-current on the right button; and
	 * the vendored stylesheet paints a hover highlight of its own until a
	 * component says it is driving one, which is what the initialised marker
	 * says. Both are idempotent, and neither gates any behaviour.
	 */

	function each(scope, selector, fn) {
		if (scope.nodeType === 1 && scope.matches(selector)) fn(scope);
		var found = scope.querySelectorAll(selector);
		for (var i = 0; i < found.length; i++) fn(found[i]);
	}

	function stamp(scope) {
		var node = scope && (scope.nodeType === 1 || scope.nodeType === 9) ? scope : document;
		var mode = document.documentElement.getAttribute('data-theme') || 'auto';

		each(node, '[data-theme-mode]', function (button) {
			button.setAttribute('aria-checked', button.getAttribute('data-theme-mode') === mode ? 'true' : 'false');
		});
		each(node, '[data-combobox]', function (box) {
			box.setAttribute('data-combobox-initialized', 'true');
		});
		each(node, '[data-command]', function (palette) {
			palette.setAttribute('data-command-initialized', 'true');
		});
	}

	/* ---- the registry ------------------------------------------------------
	 *
	 * Everything above is a behaviour this file ships. This is how an
	 * application adds one of its own without an inline handler.
	 *
	 * An application serves a script of its own -- registered with
	 * view.RegisterAsset, from the origin, like every other asset -- and calls:
	 *
	 *     arandu.ui.action('archive-message', function (event, element) { ... });
	 *
	 *     arandu.ui.define('message-actions', {
	 *         mounted:   function (ctx) { ... },
	 *         updated:   function (ctx) { ... },
	 *         destroyed: function (ctx) { ... },
	 *     });
	 *
	 * The markup names them and carries nothing else:
	 *
	 *     <div data-kyse-behavior="message-actions" data-kyse-props='{"confirm":true}'>
	 *       <button data-kyse-on-click="archive-message">Archive</button>
	 *
	 * # Why a name and not the code
	 *
	 * The attribute holds a key into a map. Nothing here parses it, compiles it
	 * or evaluates it, so the policy stays script-src 'self' with no
	 * unsafe-eval and this file keeps the property that made it exist instead of
	 * Alpine. A name that is not registered does nothing and says so once in the
	 * console -- which is a page missing a behaviour, never a page running one
	 * somebody typed into a form.
	 *
	 * It is the same shape as the effects catalogue below: a catalogue rather
	 * than a parser is what keeps an attribute data instead of code.
	 */

	var actions = {};
	var behaviours = {};

	arandu.ui.action = function (name, fn) {
		if (typeof name !== 'string' || typeof fn !== 'function') return;
		actions[name] = fn;
	};

	arandu.ui.define = function (name, hooks) {
		if (typeof name !== 'string' || !hooks) return;
		behaviours[name] = hooks;
		/* Registration can arrive after the markup: a deferred application
		 * script runs once, and by then the document is parsed. Mounting what
		 * is already on the page is what makes the order not matter. */
		mount(document);
	};

	/* named answers a lookup and reports a miss once per name.
	 *
	 * Once, because the alternative is a console line per element per swap on a
	 * page whose behaviour is misspelled -- which buries the first one, and the
	 * first one is the whole message.
	 *
	 * And not before the page has loaded. This file and the application's script
	 * are both deferred, so the first sweep runs before a single define() has;
	 * warning there reported every behaviour on the page as unregistered, on
	 * every load, moments before registering all of them. What is left after
	 * window load is a real miss, and the sweep below says so. */
	var reported = {};
	var loaded = false;

	function named(map, kind, name) {
		if (Object.prototype.hasOwnProperty.call(map, name)) return map[name];
		if (loaded) miss(kind, name);
		return null;
	}

	function miss(kind, name) {
		if (reported[kind + ':' + name]) return;
		reported[kind + ':' + name] = true;
		if (window.console && console.warn) {
			console.warn('arandu.ui: no ' + kind + ' is registered as "' + name + '"');
		}
	}

	/* context is what a hook receives: the element, and the props the server
	 * wrote beside it.
	 *
	 * The props are parsed once and kept on the element, so updated and
	 * destroyed see the same object mounted did -- a hook that stored something
	 * on ctx.props finds it there later, which is the only place per-element
	 * state can live without this file keeping a second copy of the DOM. */
	function context(element) {
		if (!element.__kyse) {
			var props = {};
			var raw = element.getAttribute('data-kyse-props');
			if (raw) {
				try {
					props = JSON.parse(raw);
				} catch (e) {
					/* The server encodes this, so a parse failure is a bug here
					 * rather than something a visitor did. The behaviour still
					 * mounts, with no props, because half a page is worse. */
					if (window.console && console.warn) {
						console.warn('arandu.ui: the props of "' + element.getAttribute('data-kyse-behavior') + '" are not JSON');
					}
				}
			}
			element.__kyse = { element: element, props: props };
		}
		return element.__kyse;
	}

	/* justMounted holds what mounted since the last settle finished.
	 *
	 * htmx fires htmx:load from a settle task and htmx:afterSettle right after
	 * the tasks run -- `se(l.tasks,…);se(l.elts,…afterSettle…)` in the bundle --
	 * so without this every element that arrived in a swap got mounted and then
	 * immediately updated. Two hooks that always fire together are one hook, and
	 * a behaviour that did its setting up in mounted did it twice. */
	var justMounted = new WeakSet();

	function mount(scope) {
		var node = scope && (scope.nodeType === 1 || scope.nodeType === 9) ? scope : document;
		each(node, '[data-kyse-behavior]', function (element) {
			if (element.getAttribute('data-kyse-mounted') === 'true') return;
			var hooks = named(behaviours, 'behaviour', element.getAttribute('data-kyse-behavior'));
			if (!hooks) return;
			element.setAttribute('data-kyse-mounted', 'true');
			justMounted.add(element);
			if (typeof hooks.mounted === 'function') hooks.mounted(context(element));
		});
	}

	function update(scope) {
		var node = scope && (scope.nodeType === 1 || scope.nodeType === 9) ? scope : document;
		each(node, '[data-kyse-behavior][data-kyse-mounted="true"]', function (element) {
			if (justMounted.has(element)) {
				justMounted.delete(element);
				return;
			}
			var hooks = behaviours[element.getAttribute('data-kyse-behavior')];
			if (hooks && typeof hooks.updated === 'function') hooks.updated(context(element));
		});
	}

	function destroy(element) {
		if (!element || element.nodeType !== 1) return;
		each(element, '[data-kyse-behavior][data-kyse-mounted="true"]', function (mounted) {
			var hooks = behaviours[mounted.getAttribute('data-kyse-behavior')];
			if (hooks && typeof hooks.destroyed === 'function') hooks.destroyed(context(mounted));
			mounted.removeAttribute('data-kyse-mounted');
			mounted.__kyse = null;
		});
	}

	/* dispatch runs the action named for this event, if there is one.
	 *
	 * The attribute is data-kyse-on-<event>, so one lookup per delegated
	 * listener covers every action for that event on the page. */
	function dispatch(event, type) {
		var from = origin(event);
		if (!from) return;
		var element = from.closest('[data-kyse-on-' + type + ']');
		if (!element) return;
		var fn = named(actions, 'action', element.getAttribute('data-kyse-on-' + type));
		if (fn) fn(event, element);
	}

	/* ---- delegation --------------------------------------------------------
	 *
	 * Six listeners, all on document, all reading attributes rather than
	 * running them. Change and submit carry no behaviour of this file's own and
	 * exist for the registry: a form is submitted and a select is changed, and
	 * neither reaches a click listener.
	 */

	document.addEventListener('change', function (event) { dispatch(event, 'change'); });
	document.addEventListener('submit', function (event) { dispatch(event, 'submit'); });

	document.addEventListener('click', function (event) {
		dispatch(event, 'click');

		var from = origin(event);
		if (!from) return;

		var copyButton = from.closest('[data-copy]');
		if (copyButton) copy(copyButton);

		var mode = from.closest('[data-theme-mode]');
		if (mode) setMode(mode.getAttribute('data-theme-mode'));

		var option = from.closest('[data-combobox] ' + OPTION);
		if (option) chooseOption(option.closest('[data-combobox]'), option);

		/* Clicking away closes: the outside click every combobox on the page
		 * cares about, decided by containment rather than by a listener each. */
		var boxes = document.querySelectorAll('[data-combobox]');
		for (var i = 0; i < boxes.length; i++) {
			if (!boxes[i].contains(from) && comboboxOpen(boxes[i])) setComboboxOpen(boxes[i], false);
		}

		var input = from.closest('[data-combobox] input[role="combobox"]');
		if (input) setComboboxOpen(input.closest('[data-combobox]'), true);
	});

	document.addEventListener('input', function (event) {
		dispatch(event, 'input');

		var from = origin(event);
		if (!from) return;

		var track = from.closest('[data-slider-track]');
		if (track) paintSlider(track);

		var boxInput = from.closest('[data-combobox] input[role="combobox"]');
		if (boxInput) {
			var box = boxInput.closest('[data-combobox]');
			var parts = comboboxParts(box);
			setComboboxOpen(box, true);
			setActive(boxInput, null, parts.listbox, OPTION, false);
		}

		var paletteInput = from.closest('[data-command] input[role="combobox"]');
		if (paletteInput) filterCommand(paletteInput.closest('[data-command]'));
	});

	document.addEventListener('keydown', function (event) {
		dispatch(event, 'keydown');

		var from = origin(event);
		if (!from || !from.matches('input[role="combobox"]')) return;

		var box = from.closest('[data-combobox]');
		if (box) { comboboxKey(event, box, from); return; }

		var palette = from.closest('[data-command]');
		if (palette) commandKey(event, palette, from);
	});

	/* The mouse moves the active item so that the keyboard and the pointer
	 * never highlight two different rows. */
	document.addEventListener('pointermove', function (event) {
		var from = origin(event);
		if (!from) return;

		var option = from.closest('[data-combobox] ' + OPTION);
		if (option) {
			if (!usable(option)) return;
			var box = option.closest('[data-combobox]');
			var parts = comboboxParts(box);
			if (activeID(parts.input) !== option.id) {
				setActive(parts.input, option, parts.listbox, OPTION, false);
			}
			return;
		}

		var line = from.closest('[data-command] ' + LINE);
		if (line) {
			if (!usable(line)) return;
			var palette = line.closest('[data-command]');
			var pieces = commandParts(palette);
			if (activeID(pieces.input) !== line.id) {
				setActive(pieces.input, line, pieces.menu, LINE, false);
			}
		}
	});

	/* ---------------------------------------------------------------------
	 * Motion, on the platform's own animation engine.
	 *
	 * CSS covers almost everything a page needs: a hover, a transition, and --
	 * with animation-timeline: view() -- an element that arrives as it is
	 * scrolled to. Two things it does not cover well are a sequence whose steps
	 * are offset from one another, and an animation that has to run backwards on
	 * the way out. Those are what this is for, and it is why the list of what it
	 * can do is short: an effect CSS already expresses belongs in the stylesheet,
	 * where it survives this file failing to load.
	 *
	 * It is Element.animate -- the Web Animations API, which every current
	 * browser ships. That matters beyond taste: the animation libraries a page
	 * would otherwise reach for are built on this same call, and what they add is
	 * a nicer way to write it. Writing it directly costs the sugar and saves the
	 * dependency, the bytes, and the policy exception a third-party script would
	 * need under script-src 'self'.
	 *
	 * It reads no expression, like the rest of this file. `data-stagger` names an
	 * effect from a closed list, and `data-stagger-step` and
	 * `data-stagger-duration` are numbers clamped to a sane range; anything else
	 * is ignored. A catalogue rather than a parser is what keeps an attribute
	 * data instead of code.
	 *
	 * Nothing here is required for a page to be usable. Every element is at its
	 * final state before this runs and is put back to it if anything goes wrong,
	 * so a browser without IntersectionObserver, a reader who asked for less
	 * motion, and this file failing to parse all produce the same page: the one
	 * with everything already in place.
	 */
	var EFFECTS = {
		rise: [
			{ opacity: 0, transform: 'translateY(20px)' },
			{ opacity: 1, transform: 'none' }
		],
		fade: [
			{ opacity: 0 },
			{ opacity: 1 }
		],
		/* Used for a row of cards: each one arrives a little scaled down and a
		 * little low, so it settles into place rather than blinking on. The scale
		 * reads as depth and the small rise carries the eye; on their own each was
		 * too slight to see, which is the whole of the "did it animate?" report. */
		settle: [
			{ opacity: 0, transform: 'scale(.94) translateY(10px)' },
			{ opacity: 1, transform: 'none' }
		]
	};

	var STAGGER_MS = 70;

	/* Long enough to read as motion. At 460ms with the slight transforms above,
	 * the animation was over before the eye found it -- a reader would ask whether
	 * anything moved. The distance grew and the duration with it; the ease still
	 * starts fast and settles, so the row does not feel slow, it feels placed. */
	var DURATION_MS = 640;
	var EASE = 'cubic-bezier(.16,1,.3,1)';

	/* Whether motion is wanted at all.
	 *
	 * Asked at the moment of use rather than once at load, because a reader can
	 * change the setting without reloading the page, and the honest answer is the
	 * one the system gives now. */
	function motionWanted() {
		return !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
	}

	/* Runs one container's children, offset from one another.
	 *
	 * The container is unobserved before the first frame rather than after the
	 * last: this runs once by design, and an element that scrolled out and back
	 * in mid-animation would otherwise restart from nothing while the reader was
	 * looking at it. */
	function runStagger(container) {
		var name = container.getAttribute('data-stagger');
		var frames = EFFECTS[name] || EFFECTS.rise;

		var step = parseInt(container.getAttribute('data-stagger-step'), 10);
		if (!(step >= 0 && step <= 400)) step = STAGGER_MS;

		/* A grid whose cards want a slower, more deliberate arrival than the row
		 * default sets data-stagger-duration; out of range or absent, the default
		 * stands. The component catalogue is the first caller: its cards read as
		 * hurried at the shared 640ms. */
		var duration = parseInt(container.getAttribute('data-stagger-duration'), 10);
		if (!(duration >= 200 && duration <= 2000)) duration = DURATION_MS;

		var children = container.children;
		for (var i = 0; i < children.length; i++) {
			children[i].animate(frames, {
				duration: duration,
				delay: i * step,
				easing: EASE,
				/* No fill. The element is already at its final state in the
				 * document, so the animation plays and hands it back rather
				 * than holding it: an animation that ends is an element the
				 * browser stops compositing. */
				fill: 'none'
			});
		}
	}

	function watchStagger(root) {
		if (!motionWanted()) return;
		if (typeof IntersectionObserver !== 'function') return;
		if (!Element.prototype.animate) return;

		var containers = root.querySelectorAll('[data-stagger]:not([data-stagger-done])');
		if (!containers.length) return;

		var seen = new IntersectionObserver(function (entries) {
			for (var i = 0; i < entries.length; i++) {
				if (!entries[i].isIntersecting) continue;
				var container = entries[i].target;
				seen.unobserve(container);
				container.setAttribute('data-stagger-done', '');
				runStagger(container);
			}
		}, { rootMargin: '0px 0px -12% 0px' });

		for (var j = 0; j < containers.length; j++) seen.observe(containers[j]);
	}

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', function () { stamp(document); watchStagger(document); mount(document); });
	} else {
		stamp(document);
		watchStagger(document);
		mount(document);
	}

	/* htmx:load fires for markup that has just been inserted, which is where a
	 * behaviour on it is mounted. The two sweeps beside it were always
	 * idempotent; mount is too, by the marker it writes.
	 *
	 * afterSettle fires once the swap has settled, on markup that may have been
	 * there before -- so it is updated and never mounted, and an element that
	 * arrived in this same swap has already had mounted called by the line
	 * above rather than updated, which is the distinction the two hooks are
	 * for.
	 *
	 * beforeCleanupElement is the one hook this file has that is not a sweep:
	 * htmx calls it with an element it is about to remove, which is the last
	 * moment a behaviour can give back a timer, an observer or a listener it
	 * took. Without it the only cost is a leak, which is the kind that is
	 * invisible until a page has been open for an hour. */
	document.addEventListener('htmx:load', function (event) {
		stamp(event.target);
		watchStagger(event.target);
		mount(event.target);
	});
	document.addEventListener('htmx:afterSettle', function (event) { update(event.target); });
	document.addEventListener('htmx:beforeCleanupElement', function (event) { destroy(event.target); });

	/* The one-time code, typed one square at a time.
	 *
	 * The squares are the visible inputs and none of them submits; a hidden
	 * input beside them carries the whole code under the field's name, kept in
	 * step on every keystroke. So the server reads one field and never has to
	 * join six of them.
	 *
	 * # What it does about the small things
	 *
	 * Typing moves to the next square, and backspace on an empty one moves back
	 * -- otherwise fixing a mistake means clicking. Arrow keys walk the row. A
	 * code pasted into any square fills all of them, because that is what
	 * copying six digits out of a message means, and a code longer than the
	 * field is cut rather than refused.
	 *
	 * # It does not decide
	 *
	 * Whether the code is the right one is the server's answer, and nothing here
	 * asks. data-code-complete says every square is filled, which is a count.
	 */
	arandu.ui.define('one-time-code', {
		mounted: function (ctx) {
			var root = ctx.element;
			var squares = Array.prototype.slice.call(root.querySelectorAll('[data-part="square"]'));
			var raw = root.querySelector('[data-part="raw"]');
			if (!squares.length) return;

			var accepts = ctx.props.alphanumeric ? /[0-9a-zA-Z]/ : /[0-9]/;

			function collect() {
				var code = squares.map(function (one) { return one.value; }).join('');
				if (raw) raw.value = code;
				root.setAttribute('data-code-complete', code.length === squares.length ? 'true' : 'false');
			}

			function spread(text, from) {
				var kept = String(text).split('').filter(function (one) { return accepts.test(one); });
				for (var i = 0; i < kept.length && from + i < squares.length; i++) {
					squares[from + i].value = kept[i];
				}
				var landed = Math.min(from + kept.length, squares.length - 1);
				squares[landed].focus();
				collect();
			}

			ctx.otp = { onInput: [], onKeyDown: [], onPaste: [], onFocus: [] };

			squares.forEach(function (square, at) {
				var onInput = function () {
					var typed = square.value;
					if (typed.length > 1) {
						/* More than one character in a box that takes one is a
						 * paste the browser routed here as input. */
						square.value = '';
						spread(typed, at);
						return;
					}
					if (typed && !accepts.test(typed)) {
						square.value = '';
						collect();
						return;
					}
					if (typed && at + 1 < squares.length) {
						squares[at + 1].focus();
					}
					collect();
				};

				var onKeyDown = function (event) {
					if (event.key === 'Backspace' && !square.value && at > 0) {
						/* Backspace on an empty square goes back and clears the
						 * one it lands on: otherwise fixing a mistake means
						 * pressing it twice. */
						event.preventDefault();
						squares[at - 1].value = '';
						squares[at - 1].focus();
						collect();
						return;
					}
					if (event.key === 'ArrowLeft' && at > 0) {
						event.preventDefault();
						squares[at - 1].focus();
					}
					if (event.key === 'ArrowRight' && at + 1 < squares.length) {
						event.preventDefault();
						squares[at + 1].focus();
					}
				};

				var onPaste = function (event) {
					event.preventDefault();
					var text = (event.clipboardData || window.clipboardData).getData('text');
					spread(text, at);
				};

				/* Selecting what is in the square means the next keystroke
				 * replaces it, which is what somebody correcting one digit
				 * expects. */
				var onFocus = function () { square.select(); };

				square.addEventListener('input', onInput);
				square.addEventListener('keydown', onKeyDown);
				square.addEventListener('paste', onPaste);
				square.addEventListener('focus', onFocus);
				ctx.otp.onInput.push(onInput);
				ctx.otp.onKeyDown.push(onKeyDown);
				ctx.otp.onPaste.push(onPaste);
				ctx.otp.onFocus.push(onFocus);
			});

			collect();
		},
		destroyed: function (ctx) {
			if (!ctx.otp) return;
			var squares = Array.prototype.slice.call(
				ctx.element.querySelectorAll('[data-part="square"]'));
			squares.forEach(function (square, at) {
				square.removeEventListener('input', ctx.otp.onInput[at]);
				square.removeEventListener('keydown', ctx.otp.onKeyDown[at]);
				square.removeEventListener('paste', ctx.otp.onPaste[at]);
				square.removeEventListener('focus', ctx.otp.onFocus[at]);
			});
		},
	});

	/* The mask, which formats a field as somebody types in it.
	 *
	 * The pattern is a string of tokens, and the vocabulary is the one anybody
	 * who has written a mask before already knows:
	 *
	 *     0  a digit, required
	 *     9  a digit, optional
	 *     #  a digit, repeated to the end
	 *     A  a letter or a digit
	 *     S  a letter
	 *
	 * Everything else in the pattern is written through as it stands.
	 *
	 * # What the form sends is the raw value
	 *
	 * The visible field shows 123.456.789-00 and is not the field that submits.
	 * Beside it is a hidden input carrying 12345678900, kept in step on every
	 * keystroke, and that is what has the form name. So nothing on the server
	 * has to know a mask existed, and no query stores punctuation because
	 * somebody forgot to strip it in one of the places that read the field.
	 *
	 * The component writes both, so a caller cannot get half of it.
	 *
	 * # Alternatives
	 *
	 * A field that takes either of two shapes is given both, and the shortest
	 * one that still holds what has been typed is the one applied. A phone
	 * number grows into its longer form at the digit that needs it rather than
	 * jumping there.
	 *
	 * # It is not validation
	 *
	 * A mask says what may be typed. Whether what was typed means anything is
	 * the server's answer, and this asks nothing of it -- the same division the
	 * password behaviour above keeps.
	 */
	arandu.ui.define('mask', {
		mounted: function (ctx) {
			var input = ctx.element.matches('input') ? ctx.element : ctx.element.querySelector('input:not([type="hidden"])');
			if (!input) return;

			var patterns = maskPatterns(ctx.props);
			if (!patterns.length) return;

			var raw = ctx.element.querySelector('[data-part="raw"]');

			var reverse = ctx.props.reverse === true;

			ctx.format = function () {
				var pattern = maskFor(patterns, input.value);
				var before = input.value;
				var caret = input.selectionStart;
				var formatted = maskApply(pattern, before, reverse);

				if (formatted !== before) {
					input.value = formatted;
					if (reverse) {
						/* Filling from the right means the caret belongs at the
						 * end: every keystroke pushes what came before it left. */
						input.setSelectionRange(formatted.length, formatted.length);
					} else {
						/* The caret moves by however many characters the mask
						 * inserted before it, so typing in the middle of a value
						 * does not throw the cursor to the end. */
						var typed = maskUnmask(pattern, before.slice(0, caret)).length;
						var at = maskCaretFor(pattern, typed);
						input.setSelectionRange(at, at);
					}
				}
				if (raw) raw.value = maskUnmask(pattern, input.value);

				/* The field says whether the pattern is filled, so a stylesheet
				 * can show it and nothing has to ask this file. It is not a
				 * verdict: the server decides whether the value means anything,
				 * and a complete shape is only a shape. */
				input.setAttribute('data-mask-complete',
					maskComplete(pattern, input.value) ? 'true' : 'false');
			};

			input.addEventListener('input', ctx.format);
			/* Emptying a field that never matched, on the way out, is what the
			 * plugin this borrows its vocabulary from calls clearIfNotMatch. A
			 * half-typed value left behind is a value somebody submits without
			 * looking. */
			if (ctx.props.clearIfNotMatch === true) {
				input.addEventListener('blur', function () {
					var pattern = maskFor(patterns, input.value);
					if (input.value && !maskComplete(pattern, input.value)) {
						input.value = '';
						if (raw) raw.value = '';
						input.setAttribute('data-mask-complete', 'false');
					}
				});
			}
			/* A paste arrives as an input event in every browser this ships to,
			 * so there is nothing extra to listen for -- and a value the server
			 * rendered is formatted once, here, rather than left raw until
			 * somebody types. */
			ctx.format();
		},
		destroyed: function (ctx) {
			var input = ctx.element.matches('input') ? ctx.element : ctx.element.querySelector('input:not([type="hidden"])');
			if (input && ctx.format) input.removeEventListener('input', ctx.format);
		},
	});

	/* maskTokens is the dictionary, and it is closed: a mask needing a sixth
	 * token would be a mask nobody can read without this table beside them. */
	var maskTokens = {
		'0': { accepts: /[0-9]/ },
		'9': { accepts: /[0-9]/, optional: true },
		'#': { accepts: /[0-9]/, repeating: true },
		'A': { accepts: /[0-9a-zA-Z]/ },
		'S': { accepts: /[a-zA-Z]/ },
	};

	/* maskPatterns reads the pattern or patterns out of the props. */
	function maskPatterns(props) {
		var declared = (props || {}).pattern;
		if (typeof declared === 'string') return declared ? [declared] : [];
		if (Object.prototype.toString.call(declared) === '[object Array]') {
			return declared.filter(function (one) { return typeof one === 'string' && one; });
		}
		return [];
	}

	/* maskCapacity is how many characters a pattern's tokens accept, and -1 for
	 * one that repeats and therefore has no end. */
	function maskCapacity(pattern) {
		var n = 0;
		for (var i = 0; i < pattern.length; i++) {
			var token = maskTokens[pattern.charAt(i)];
			if (!token) continue;
			if (token.repeating) return -1;
			n++;
		}
		return n;
	}

	/* maskFor picks the shortest pattern that still holds what was typed. */
	function maskFor(patterns, value) {
		if (patterns.length === 1) return patterns[0];

		var widest = patterns[0];
		for (var i = 0; i < patterns.length; i++) {
			if (maskCapacity(patterns[i]) > maskCapacity(widest)) widest = patterns[i];
		}
		var typed = maskUnmask(widest, value).length;

		var best = null;
		for (var j = 0; j < patterns.length; j++) {
			var size = maskCapacity(patterns[j]);
			if (size < 0 || size < typed) continue;
			if (best === null || size < maskCapacity(best)) best = patterns[j];
		}
		return best || widest;
	}

	/* maskApply formats a value, dropping what the pattern cannot accept.
	 *
	 * Dropped rather than refused: what somebody pastes is usually the
	 * formatted value, and refusing its punctuation would mean refusing a paste
	 * of exactly what this produces. */
	function maskApply(pattern, value, reverse) {
		if (reverse) return maskApplyReverse(pattern, value);
		var out = '';
		var at = 0;
		for (var cursor = 0; cursor < pattern.length && at < value.length;) {
			var symbol = pattern.charAt(cursor);
			var token = maskTokens[symbol];
			if (!token) {
				out += symbol;
				if (value.charAt(at) === symbol) at++;
				cursor++;
				continue;
			}
			if (!token.accepts.test(value.charAt(at))) { at++; continue; }
			out += value.charAt(at);
			at++;
			if (!token.repeating) cursor++;
		}
		/* A separator with nothing behind it is punctuation somebody is about to
		 * type past, and showing it moves the caret for no reason. */
		while (out.length && !/[0-9a-zA-Z]/.test(out.charAt(out.length - 1))) {
			out = out.slice(0, -1);
		}
		return out;
	}

	/* maskApplyReverse fills a pattern from the right.
	 *
	 * It is how money is typed: the first digit somebody enters is the last one
	 * of the value, and every one after pushes the rest left past the
	 * separators. Filling from the left instead would put the first keystroke
	 * in the thousands and read 1 as 1.000,00.
	 *
	 * A separator with nothing in front of it is dropped for the same reason a
	 * trailing one is: it is punctuation the next keystroke is about to fill. */
	function maskApplyReverse(pattern, value) {
		var out = '';
		var at = value.length - 1;
		for (var cursor = pattern.length - 1; cursor >= 0 && at >= 0;) {
			var symbol = pattern.charAt(cursor);
			var token = maskTokens[symbol];
			if (!token) {
				out = symbol + out;
				if (value.charAt(at) === symbol) at--;
				cursor--;
				continue;
			}
			if (!token.accepts.test(value.charAt(at))) { at--; continue; }
			out = value.charAt(at) + out;
			at--;
			if (!token.repeating) cursor--;
		}
		while (out.length && !/[0-9a-zA-Z]/.test(out.charAt(0))) {
			out = out.slice(1);
		}
		return out;
	}

	/* maskUnmask gives back what the form sends: the characters the tokens
	 * accept, with everything the pattern writes through removed. */
	function maskUnmask(pattern, value) {
		var out = '';
		var at = 0;
		for (var cursor = 0; cursor < pattern.length && at < value.length;) {
			var symbol = pattern.charAt(cursor);
			var token = maskTokens[symbol];
			if (!token) {
				if (value.charAt(at) === symbol) at++;
				cursor++;
				continue;
			}
			if (!token.accepts.test(value.charAt(at))) { at++; continue; }
			out += value.charAt(at);
			at++;
			if (!token.repeating) cursor++;
		}
		return out;
	}

	/* maskComplete reports whether every required token is filled.
	 *
	 * Required, so a pattern whose tail is optional is complete without it:
	 * 0009 is complete at three digits. It answers about the shape and nothing
	 * else -- eleven digits are eleven digits, and whether they mean anything is
	 * a question with arithmetic in it that belongs to whoever owns the
	 * document. */
	function maskComplete(pattern, value) {
		var required = 0;
		for (var i = 0; i < pattern.length; i++) {
			var token = maskTokens[pattern.charAt(i)];
			if (!token || token.optional) continue;
			required++;
			if (token.repeating) break;
		}
		return maskUnmask(pattern, value).length >= required;
	}

	/* maskCaretFor is where the caret sits after a given number of accepted
	 * characters: past the separators that precede them. */
	function maskCaretFor(pattern, typed) {
		if (typed <= 0) return 0;
		var seen = 0;
		for (var i = 0; i < pattern.length; i++) {
			var token = maskTokens[pattern.charAt(i)];
			if (token) {
				seen++;
				if (seen >= typed) return i + 1;
			}
		}
		return pattern.length;
	}

	/* The password box, which is the one behaviour this file owns.
	 *
	 * Every other behaviour is the application's: this file registers two by way
	 * of example and nothing else. This one is different because the component
	 * that emits it ships in kyse, so a project that draws a password field
	 * writes data-kyse-behavior="password" without having asked for it -- and
	 * until now nothing registered that name. The panel never opened, the
	 * checklist never ticked, and the console said the behaviour was missing on
	 * every page with a sign-up form on it. A component that emits a name its
	 * own ecosystem does not answer is a component with a hole in it, and the
	 * hole belongs here rather than in every project.
	 *
	 * The props are the policy's AppliedRules verbatim -- min, max, letters,
	 * mixedCase, numbers, symbols, uncompromised -- keyed exactly as the
	 * requirement lines are, so a line is matched to its rule by name and never
	 * by position or by its wording.
	 *
	 * # What it does not do
	 *
	 * It does not decide. The server validates the password when the form is
	 * submitted, with the same policy, and nothing here is consulted for that:
	 * a checklist is a courtesy to whoever is typing, and treating it as the
	 * rule would put the decision in the browser. uncompromised is the plainest
	 * case -- only the server can answer it -- so its line never ticks here.
	 */
	arandu.ui.define('password', {
		mounted: function (ctx) {
			var root = ctx.element;
			var input = root.querySelector('[data-part="input"]');
			if (!input) return;

			var reveal = root.querySelector('[data-part="reveal"]');
			if (reveal) {
				reveal.addEventListener('click', function () {
					var hidden = input.type === 'password';
					input.type = hidden ? 'text' : 'password';
					reveal.setAttribute('aria-pressed', hidden ? 'true' : 'false');
					/* The two labels are in the markup, so this file carries no
					 * sentence and translates nothing. */
					toggle(reveal.querySelector('[data-reveal-shown]'), hidden);
					toggle(reveal.querySelector('[data-reveal-hidden]'), !hidden);
				});
			}

			ctx.judge = function () { judge(root, input.value, ctx.props || {}); };
			input.addEventListener('input', ctx.judge);
			ctx.judge();
		},
		destroyed: function (ctx) {
			var input = ctx.element.querySelector('[data-part="input"]');
			if (input && ctx.judge) input.removeEventListener('input', ctx.judge);
		},
	});

	/* toggle shows or hides one of the reveal labels. */
	function toggle(element, shown) {
		if (element) element.hidden = !shown;
	}

	/* judge ticks the lines a password already satisfies and moves the meter.
	 *
	 * The meter is the count of satisfied requirements over the number of them,
	 * and not a score: a number nobody can derive from what is on the screen is
	 * a number nobody can act on, and "why is it orange" has to have an answer
	 * the checklist gives.
	 */
	function judge(root, value, rules) {
		var met = {
			min: typeof rules.min !== 'number' || value.length >= rules.min,
			max: typeof rules.max !== 'number' || value.length <= rules.max,
			letters: !rules.letters || /\p{L}/u.test(value),
			mixedCase: !rules.mixedCase || (/\p{Lu}/u.test(value) && /\p{Ll}/u.test(value)),
			numbers: !rules.numbers || /\p{N}/u.test(value),
			symbols: !rules.symbols || /[^\p{L}\p{N}\s]/u.test(value),
			/* Only the server knows, so this line never ticks here. */
			uncompromised: false,
		};

		var total = 0;
		var satisfied = 0;
		each(root, '[data-requirement]', function (line) {
			var key = line.getAttribute('data-requirement');
			var ok = met[key] === true;
			total += 1;
			if (ok) satisfied += 1;
			line.setAttribute('data-met', ok ? 'true' : 'false');
			var done = line.querySelector('[data-part="done"]');
			if (done) done.hidden = !ok;
		});

		var fill = root.querySelector('[data-part="fill"]');
		if (fill) {
			fill.style.width = total === 0 ? '0%' : Math.round((satisfied / total) * 100) + '%';
		}
		var meter = root.querySelector('[data-part="meter"]');
		if (meter) {
			meter.setAttribute('aria-valuenow', String(satisfied));
			meter.setAttribute('aria-valuemax', String(total));
		}
		/* An empty box has nothing to say about itself, so the panel that
		 * explains the policy stays open and the strength summary stays quiet. */
		var strength = root.querySelector('[data-part="strength"]');
		if (strength) strength.setAttribute('data-met', total > 0 && satisfied === total ? 'true' : 'false');
	}

	/* By window load every deferred script has run, so a behaviour still
	 * unmounted is one nobody registered -- which is worth one line each, and is
	 * the only moment this file can tell that apart from a script that has not
	 * run yet. From here on a miss is reported where it is found. */
	window.addEventListener('load', function () {
		loaded = true;
		mount(document);
		each(document, '[data-kyse-behavior]', function (element) {
			if (element.getAttribute('data-kyse-mounted') !== 'true') {
				miss('behaviour', element.getAttribute('data-kyse-behavior'));
			}
		});
	});
})();
