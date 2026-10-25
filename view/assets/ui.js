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

	/* The two ways in, declared before anything uses them.
	 *
	 * Every behaviour in this file registers itself by calling define, and the
	 * first of those calls is a few hundred lines above where these used to
	 * sit. So the first one threw -- define was not a function yet -- and the
	 * throw took the rest of the file with it: no behaviour registered, no
	 * delegated attribute listened, and the page kept every control it draws
	 * and lost everything they do. The theme buttons were the visible half of
	 * that, because they are the ones somebody presses first.
	 *
	 * mount is a function declaration and is reachable from here whatever
	 * order it is written in; these are assignments and are not.
	 */
	var actions = {};
	var behaviours = {};

	// Native registration may synchronously format existing masked inputs.
	var maskTokens = {
		'0': { accepts: /[0-9]/ },
		'9': { accepts: /[0-9]/, optional: true },
		'#': { accepts: /[0-9]/, repeating: true },
		'A': { accepts: /[0-9a-zA-Z]/ },
		'S': { accepts: /[a-zA-Z]/ },
	};

	// define() can mount existing elements before the rest of this file runs.
	var justMounted = new WeakSet();

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

	/* ---- table ---------------------------------------------------------------
	 *
	 * The data table is a region the server draws. Searching, ordering, the
	 * facets, the page and the page size are addresses: the search is a form,
	 * the headers and the steps are links, and htmx swaps what the server
	 * answers with. This is what the browser does that an address cannot.
	 *
	 *   - menus: the columns menu, a facet's popover, a header's menu and a
	 *     row's menu are <details> the markup draws, opened here so the panel
	 *     can be placed beside its button, outside the table's own clipping;
	 *   - the search: one request, a short while after typing stops, and none
	 *     when Enter is pressed after it;
	 *   - the selection: the keys ticked, kept across a search, a new order, a
	 *     facet and a page, because the rows are swapped and the keys are not;
	 *   - the swap itself: an open menu stays open, the address bar says the
	 *     canonical address of what is on screen, and the focus stays in the
	 *     search box;
	 *   - a complete list: when the page holds every row, the server says so
	 *     (data-complete) and the browser orders, searches, filters and pages
	 *     the rows in hand. That is the only case where ordering rows here is
	 *     right, and it is the only case this does it.
	 *
	 * No class is added by a script. Anything a script shows or hides is
	 * already in the markup, with its classes, and is switched with `hidden`,
	 * because a class written here is a class the stylesheet was never compiled
	 * with.
	 *
	 * Every sentence comes from the server, with its placeholders. Nothing here
	 * composes English.
	 */
	var tableSelections = {};
	var tableSwapping = {};
	var tableCarried = {};
	var tableTimers = {};

	function tableRootOf(node) {
		return node && node.closest ? node.closest('[data-kyse-behavior="table"]') : null;
	}

	/* The props are read through the registry's own context, so they are the
	 * same object mounted receives, and available to the swap hooks below
	 * before the new region has been mounted. */
	function tableProps(root) {
		return context(root).props || {};
	}

	function tableSay(root, name, slots) {
		var sentence = tableProps(root)[name] || '';
		for (var key in slots) {
			if (Object.prototype.hasOwnProperty.call(slots, key)) {
				sentence = sentence.split('{' + key + '}').join(slots[key]);
			}
		}
		return sentence;
	}

	/* ---- menus ---------------------------------------------------------- */

	function menuPanel(menu) {
		return menu.querySelector(':scope > [data-menu-panel]');
	}

	/* The panel is fixed, so the table's own overflow cannot cut it, and it is
	 * placed from the button's rectangle: under it, aligned to the side the
	 * markup says, turned over when there is no room below, and kept inside the
	 * window. Assigning a property of style is the CSSOM; the policy refuses a
	 * style attribute, not this. */
	function placeMenu(menu) {
		var panel = menuPanel(menu);
		var button = menu.querySelector(':scope > summary');
		if (!panel || !button) return;
		var at = button.getBoundingClientRect();
		var width = panel.offsetWidth;
		var height = panel.offsetHeight;
		var wide = document.documentElement.clientWidth;
		var tall = window.innerHeight;
		var rtl = getComputedStyle(button).direction === 'rtl';
		var right = (panel.getAttribute('data-align') === 'end') !== rtl;
		var left = right ? at.right - width : at.left;
		left = Math.max(8, Math.min(left, wide - width - 8));
		var top = at.bottom + 4;
		var flipped = top + height > tall - 8 && at.top - 4 - height >= 8;
		if (flipped) top = at.top - 4 - height;
		panel.style.left = left + 'px';
		panel.style.top = top + 'px';
		panel.setAttribute('data-side', flipped ? 'top' : 'bottom');
	}

	function openMenu(menu) {
		each(document, 'details[data-menu][open]', function (other) {
			if (other !== menu) other.open = false;
		});
		menu.open = true;
		placeMenu(menu);
	}

	function closeMenus(except) {
		each(document, 'details[data-menu][open]', function (menu) {
			if (menu !== except && !(except && menu.contains(except))) menu.open = false;
		});
	}

	function menuItems(menu) {
		var panel = menuPanel(menu);
		if (!panel) return [];
		return Array.prototype.filter.call(
			panel.querySelectorAll('a[href], button:not([disabled]), input:not([type="hidden"]):not([disabled])'),
			function (item) { return !item.closest('[hidden]') && item.getAttribute('aria-disabled') !== 'true'; }
		);
	}

	document.addEventListener('click', function (event) {
		var from = origin(event);
		if (!from) return;
		var button = from.closest('details[data-menu] > summary');
		if (button) {
			event.preventDefault();
			var menu = button.parentElement;
			if (menu.open) menu.open = false;
			else openMenu(menu);
			return;
		}
		var inside = from.closest('details[data-menu]');
		closeMenus(inside);
		/* A menu of things to do closes once one is chosen; a menu of choices
		 * that stay chosen -- the columns, a facet -- stays where it is. */
		if (inside && inside.hasAttribute('data-closes') && from.closest('[role="menuitem"]')) inside.open = false;
	}, true);

	document.addEventListener('keydown', function (event) {
		var from = origin(event);
		if (!from) return;
		var menu = from.closest('details[data-menu]');
		if (event.key === 'Escape') {
			var open = menu && menu.open ? menu : document.querySelector('details[data-menu][open]');
			if (!open) return;
			open.open = false;
			var back = open.querySelector(':scope > summary');
			if (back) back.focus();
			return;
		}
		if (!menu || !menu.open) return;
		if (event.key === 'Enter' && from.matches('[data-part="facet-search"]')) {
			event.preventDefault();
			return;
		}
		if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
		var items = menuItems(menu);
		if (!items.length) return;
		event.preventDefault();
		var at = items.indexOf(document.activeElement);
		var to = event.key === 'ArrowDown'
			? (at + 1) % items.length
			: (at <= 0 ? items.length - 1 : at - 1);
		items[to].focus();
	});

	/* htmx keeps a snapshot of the page as a string before it leaves it, and a
	 * panel placed with style.left is `style="left: …"` in that string -- which
	 * the policy refuses when the snapshot is parsed again. The menus are shut
	 * and the placement taken off before the page is saved. */
	document.addEventListener('htmx:beforeHistorySave', function () {
		each(document, 'details[data-menu]', function (menu) {
			menu.open = false;
			var panel = menuPanel(menu);
			if (panel) panel.removeAttribute('style');
		});
	});

	/* A menu follows its button, and the page under it can move: scrolling or
	 * resizing closes it, which is what a popover that is not tied to its
	 * button does. Scrolling inside the panel is not a move. */
	window.addEventListener('scroll', function (event) {
		var from = event.target;
		if (from && from.nodeType === 1 && from.closest && from.closest('[data-menu-panel]')) return;
		closeMenus(null);
	}, true);
	window.addEventListener('resize', function () { closeMenus(null); });

	/* ---- a facet's own search ------------------------------------------ */

	document.addEventListener('input', function (event) {
		var from = origin(event);
		if (!from || !from.matches('[data-part="facet-search"]')) return;
		var panel = from.closest('[data-menu-panel]');
		if (!panel) return;
		var needle = (from.value || '').trim().toLowerCase();
		var shown = 0;
		each(panel, '[data-facet-item]', function (item) {
			var text = (item.getAttribute('data-text') || item.textContent || '').toLowerCase();
			var match = !needle || text.indexOf(needle) >= 0;
			item.hidden = !match;
			if (match) shown++;
		});
		each(panel, '[data-facet-empty]', function (line) { line.hidden = shown > 0; });
	});

	/* ---- one request from the search box -------------------------------- *
	 *
	 * The form is the one owner of the state, and a request is its submit. The
	 * box asks for one a moment after typing stops, unless what is in it is what
	 * the server drew. Enter submits at once and cancels the wait, so the two
	 * never both go out. A change event from the box is not a request: it is
	 * the same text Enter or the wait is about to send, and the form would
	 * otherwise send it a second time.
	 */
	document.addEventListener('change', function (event) {
		var from = origin(event);
		if (from && from.matches('[data-part="search"], [data-part="facet-search"]')) event.stopPropagation();
	}, true);

	document.addEventListener('input', function (event) {
		var from = origin(event);
		if (!from || !from.matches('[data-part="search"]')) return;
		var root = tableRootOf(from);
		if (!root || root.getAttribute('data-complete') === 'true') return;
		var form = from.form;
		if (!form) return;
		window.clearTimeout(tableTimers[root.id]);
		tableTimers[root.id] = window.setTimeout(function () {
			if (!from.isConnected || from.value === from.defaultValue) return;
			if (form.requestSubmit) form.requestSubmit();
			else form.dispatchEvent(new Event('submit', { cancelable: true, bubbles: true }));
		}, 300);
	});

	document.addEventListener('submit', function (event) {
		var root = tableRootOf(event.target);
		if (!root) return;
		window.clearTimeout(tableTimers[root.id]);
		if (root.getAttribute('data-complete') === 'true') event.preventDefault();
	}, true);

	/* ---- the selection -------------------------------------------------- *
	 *
	 * The rows are swapped and the keys are not. The keys ticked are kept per
	 * table, the boxes on the page are drawn from them, and the keys that are
	 * not on this page ride in the bulk form as hidden fields, so one press
	 * posts the whole selection. The count is said against the server's total.
	 */
	function selectionOf(root) {
		var held = tableSelections[root.id];
		if (!held) held = tableSelections[root.id] = { keys: {}, seeded: false };
		return held;
	}

	function boxesOf(root) {
		return Array.prototype.slice.call(root.querySelectorAll('[data-part="select"]'));
	}

	function visibleBoxes(root) {
		return boxesOf(root).filter(function (box) {
			var row = box.closest('tr');
			return row && !row.hidden;
		});
	}

	function syncSelection(root) {
		if (!root.id) return;
		var held = selectionOf(root);
		var boxes = boxesOf(root);
		if (!held.seeded) {
			boxes.forEach(function (box) { if (box.checked) held.keys[box.value] = true; });
			held.seeded = true;
		}
		var onPage = {};
		boxes.forEach(function (box) {
			onPage[box.value] = true;
			box.checked = !!held.keys[box.value];
			var row = box.closest('tr');
			if (row) {
				if (box.checked) row.setAttribute('data-state', 'selected');
				else row.removeAttribute('data-state');
			}
		});

		var total = Object.keys(held.keys).length;
		var props = tableProps(root);
		var bulk = root.querySelector('[data-part="bulk"]');
		if (bulk) {
			Array.prototype.slice.call(bulk.querySelectorAll('input[data-bulk-key]')).forEach(function (one) { one.remove(); });
			Object.keys(held.keys).forEach(function (key) {
				if (onPage[key]) return;
				var hidden = document.createElement('input');
				hidden.type = 'hidden';
				hidden.name = props.selectName || '';
				hidden.value = key;
				hidden.setAttribute('data-bulk-key', '');
				bulk.appendChild(hidden);
			});
			bulk.hidden = total === 0;
		}

		var line = root.querySelector('[data-part="count"]');
		if (line && props.selectName) {
			var known = root.hasAttribute('data-total');
			var template = known
				? (line.getAttribute('data-selected-template') || props.selected || '{n} of {total} row(s) selected.')
				: (props.selectedCount || '{n} row(s) selected.');
			line.textContent = template
				.split('{n}').join(String(total))
				.split('{total}').join(root.getAttribute('data-total') || '0');
		}

		var master = root.querySelector('[data-select-all]');
		if (master) {
			var page = visibleBoxes(root);
			var ticked = page.filter(function (box) { return box.checked; }).length;
			master.checked = page.length > 0 && ticked === page.length;
			/* Indeterminate is a property and not an attribute: there is no
			 * markup for "some", which is why the server cannot draw this state
			 * and this line exists. */
			master.indeterminate = ticked > 0 && ticked < page.length;
		}
	}

	/* ---- the swap ------------------------------------------------------- *
	 *
	 * The region is replaced whole. Before it goes, what the reader had open
	 * and where the cursor was are taken down; as soon as the new region is in,
	 * they are put back, before the next frame, so a menu that stays open
	 * while its boxes are ticked never closes at all.
	 */
	document.addEventListener('htmx:beforeSwap', function (event) {
		var target = event.detail && event.detail.target;
		var root = tableRootOf(target);
		if (!root || !root.id) return;
		tableSwapping[root.id] = true;
		var open = [];
		each(root, 'details[data-menu][open]', function (menu) { if (menu.id) open.push(menu.id); });
		var carried = { menus: open, focus: null };
		var active = document.activeElement;
		if (active && active !== document.body && root.contains(active) && active.id) {
			carried.focus = {
				id: active.id,
				start: typeof active.selectionStart === 'number' ? active.selectionStart : null,
				end: typeof active.selectionEnd === 'number' ? active.selectionEnd : null,
			};
		}
		tableCarried[root.id] = carried;
	});

	document.addEventListener('htmx:afterSwap', function () {
		Object.keys(tableCarried).forEach(function (id) {
			var root = document.getElementById(id);
			var carried = tableCarried[id];
			if (!root || !carried) return;
			syncSelection(root);
			carried.menus.forEach(function (one) {
				var menu = document.getElementById(one);
				if (menu && root.contains(menu)) {
					menu.open = true;
					placeMenu(menu);
				}
			});
			if (carried.focus) {
				var field = document.getElementById(carried.focus.id);
				if (field && root.contains(field) && document.activeElement !== field) {
					field.focus();
					if (carried.focus.start !== null && field.setSelectionRange) {
						try { field.setSelectionRange(carried.focus.start, carried.focus.end); } catch (e) { /* not a text field */ }
					}
				}
			}
		});
	});

	/* The address bar says what the server says is on screen. The form
	 * serialises its fields as they are -- an empty search, the size even when
	 * it is the default -- and the server's canonical address leaves those out.
	 * Replacing the entry htmx just pushed keeps one entry per step. Only an
	 * entry htmx pushed is replaced: a control that forgot to ask for a push
	 * must not have its missing history written over the page's own. */
	document.addEventListener('htmx:pushedIntoHistory', function () {
		Object.keys(tableCarried).forEach(function (id) { tableCarried[id].pushed = true; });
	});

	document.addEventListener('htmx:afterSettle', function () {
		Object.keys(tableCarried).forEach(function (id) {
			var root = document.getElementById(id);
			var pushed = tableCarried[id].pushed;
			delete tableCarried[id];
			delete tableSwapping[id];
			if (!pushed || !root || !window.history || !history.replaceState) return;
			var canonical = root.getAttribute('data-url');
			if (!canonical) return;
			var now = location.pathname + location.search;
			if (canonical !== now && canonical.charAt(0) === '/') {
				try { history.replaceState(history.state, '', canonical + location.hash); } catch (e) { /* the entry stays as pushed */ }
			}
		});
	});

	arandu.ui.define('table', {
		mounted: function (ctx) {
			var root = ctx.element;
			var props = ctx.props || {};
			var complete = root.getAttribute('data-complete') === 'true';
			ctx.state = { page: 1 };

			/* ---- working a complete list in the browser --------------------
			 *
			 * Only when the page holds every row, which the server states with
			 * data-complete. Ordering a page of a longer list orders only what
			 * was sent, and filtering it hides matches that never arrived. When
			 * it is not complete none of this runs.
			 *
			 * The rows are moved, never rebuilt: a <tr> that is already in the
			 * document keeps every attribute, checked box and bound handler.
			 */
			var body = function () { return root.querySelector('tbody'); };
			var allRows = function () {
				var tbody = body();
				if (!tbody) return [];
				return Array.prototype.filter.call(tbody.rows, function (row) {
					return !row.hasAttribute('data-empty-row');
				});
			};
			var size = Number(props.pageSize) || 0;
			var page = 1;

			/* One collator, made once. localeCompare builds one per call, and a
			 * sort is n log n calls. */
			var collator = null;
			var compare = function (x, y) {
				if (!collator) {
					collator = typeof Intl !== 'undefined' && Intl.Collator
						? new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
						: { compare: function (a, b) { return a < b ? -1 : a > b ? 1 : 0; } };
				}
				return collator.compare(x, y);
			};

			/* The text a row is searched by, built once and kept on the row,
			 * and dropped whenever a column is hidden: a hidden column is not
			 * searched. */
			var haystack = function (row) {
				if (row.__hay === undefined) {
					var text = '';
					for (var i = 0; i < row.cells.length; i++) {
						if (row.cells[i].hidden) continue;
						text += (row.cells[i].textContent || '').toLowerCase() + ' ';
					}
					row.__hay = text;
				}
				return row.__hay;
			};
			var forget = function () {
				allRows().forEach(function (row) { row.__hay = undefined; });
			};

			/* comparable is what a cell sorts by: the value the server handed
			 * over, or the text it draws. "1.240,00" sorts before "860,00" as
			 * text and after it as money, which is why the server is asked for
			 * the comparable form. */
			var comparable = function (row, at) {
				var cell = row.cells[at];
				if (!cell) return '';
				var raw = cell.getAttribute('data-sort-value');
				return raw === null ? (cell.textContent || '').trim() : raw;
			};

			var order = function (at, dir) {
				var tbody = body();
				if (!tbody) return;
				var rows = allRows();
				var sign = dir === 'desc' ? -1 : 1;
				var keyed = new Array(rows.length);
				var numeric = false;
				var seenValue = false;
				for (var i = 0; i < rows.length; i++) {
					var raw = comparable(rows[i], at);
					var num = raw === '' ? NaN : Number(raw);
					/* An empty cell does not decide the kind of the column. */
					if (raw !== '') {
						if (!seenValue) { numeric = true; seenValue = true; }
						if (isNaN(num)) numeric = false;
					}
					keyed[i] = { row: rows[i], raw: raw, num: num, blank: raw === '' };
				}
				var by = numeric
					? function (a, b) { return (a.num - b.num) * sign; }
					: function (a, b) { return compare(a.raw, b.raw) * sign; };
				/* Blanks stay at one end whichever way the column is sorted: a
				 * row with nothing in the column is the absence of a value. */
				keyed.sort(function (a, b) {
					if (a.blank !== b.blank) return a.blank ? 1 : -1;
					if (a.blank) return 0;
					return by(a, b);
				});
				var moved = document.createDocumentFragment();
				for (var m = 0; m < keyed.length; m++) moved.appendChild(keyed[m].row);
				tbody.appendChild(moved);
			};

			/* The pager keeps its markup and the browser moves the state of it:
			 * which steps are live, and where the reader is. */
			var pager = function (pages) {
				var nav = root.querySelector('[data-part="pagination"]');
				if (!nav) return;
				nav.hidden = pages < 2;
				var live = { first: page > 1, previous: page > 1, next: page < pages, last: page < pages };
				each(nav, '[data-step]', function (step) {
					var on = live[step.getAttribute('data-step')];
					if (on) {
						step.removeAttribute('aria-disabled');
						step.removeAttribute('tabindex');
					} else {
						step.setAttribute('aria-disabled', 'true');
						step.setAttribute('tabindex', '-1');
					}
				});
				each(root, '[data-part="page-of"]', function (where) {
					where.textContent = (where.getAttribute('data-page-of-template') || props.pageOf || '{n} / {total}')
						.split('{n}').join(String(page))
						.split('{total}').join(String(pages));
				});
			};

			var facetSummary = function (facet) {
				var chosen = [];
				each(facet, '[data-part="filter"]:checked', function (box) { chosen.push(box.value); });
				var line = facet.querySelector('[data-part="facet-chosen"]');
				if (!line) return;
				var summary = facet.querySelector('summary');
				var rule = summary && summary.querySelector('[role="separator"]');
				line.hidden = chosen.length === 0;
				if (rule) rule.hidden = chosen.length === 0;
				each(line, '[data-facet-count]', function (one) { one.textContent = String(chosen.length); });
				each(line, '[data-facet-many]', function (one) {
					one.hidden = chosen.length <= 2;
					one.textContent = (props.facetSelected || '{n} selected').split('{n}').join(String(chosen.length));
				});
				each(line, '[data-facet-badge]', function (one) {
					one.hidden = chosen.length > 2 || chosen.indexOf(one.getAttribute('data-facet-badge')) < 0;
				});
			};

			var show = function () {
				var query = '';
				var box = root.querySelector('[data-part="search"]');
				if (box) query = (box.value || '').trim().toLowerCase();

				/* A row passes a facet when its own value for it is one of
				 * those ticked, and a facet with none ticked lets every row
				 * through. */
				var facets = {};
				each(root, '[data-part="filter"]:checked', function (one) {
					(facets[one.name] = facets[one.name] || []).push(one.value);
				});
				var passes = function (row) {
					for (var key in facets) {
						if (!Object.prototype.hasOwnProperty.call(facets, key)) continue;
						if (facets[key].indexOf(row.getAttribute('data-facet-' + key)) < 0) return false;
					}
					return true;
				};

				var rows = allRows();
				var hits = new Array(rows.length);
				var matched = 0;
				for (var i = 0; i < rows.length; i++) {
					hits[i] = (!query || haystack(rows[i]).indexOf(query) >= 0) && passes(rows[i]);
					if (hits[i]) matched++;
				}

				var pages = size > 0 ? Math.max(1, Math.ceil(matched / size)) : 1;
				if (page > pages) page = pages;
				var from = size > 0 ? (page - 1) * size : 0;
				var to = size > 0 ? page * size : Infinity;

				var seen = 0;
				for (var at = 0; at < rows.length; at++) {
					if (!hits[at]) { rows[at].hidden = true; continue; }
					rows[at].hidden = seen < from || seen >= to;
					seen++;
				}

				each(root, '[data-empty-row]', function (line) { line.hidden = matched !== 0; });
				root.setAttribute('data-total', String(matched));
				if (!props.selectName) {
					var count = root.querySelector('[data-part="count"]');
					if (count) count.textContent = (props.rows || '{total} row(s).').split('{total}').join(String(matched));
				}
				pager(pages);
				syncSelection(root);
			};

			/* A sortable header says which of its three states it is in. */
			var nameHeads = function () {
				each(root, 'th[aria-sort]', function (head) {
					var state = head.getAttribute('aria-sort');
					var line = state === 'ascending' ? 'ascending' : state === 'descending' ? 'descending' : 'unsorted';
					var control = head.querySelector('[data-part="sort"], summary');
					var label = '';
					if (control) {
						var shown = control.querySelector(':scope > span:not([data-sort-icon])');
						label = ((shown || control).textContent || '').replace(/\s+/g, ' ').trim();
						control.setAttribute('aria-label', tableSay(root, line, { column: label }));
					}
					var which = state === 'ascending' ? 'asc' : state === 'descending' ? 'desc' : 'none';
					each(head, '[data-sort-icon]', function (icon) {
						icon.hidden = icon.getAttribute('data-sort-icon') !== which;
					});
				});
			};

			var sortBy = function (head, dir) {
				var at = Array.prototype.indexOf.call(head.parentElement.children, head);
				each(root, 'th[aria-sort]', function (one) { one.setAttribute('aria-sort', 'none'); });
				head.setAttribute('aria-sort', dir === 'desc' ? 'descending' : 'ascending');
				nameHeads();
				order(at, dir);
				/* Reordering goes back to the first page, as the server does,
				 * or the same gesture would have two answers depending on where
				 * the rows are worked. */
				page = 1;
				show();
			};

			var hideColumn = function (key, on) {
				each(root, '[data-column="' + key + '"]', function (cell) { cell.hidden = !on; });
				each(root, '[data-column-toggle="' + key + '"]', function (item) {
					item.setAttribute('aria-checked', on ? 'true' : 'false');
					each(item, '[data-indicator]', function (check) { check.hidden = !on; });
				});
				/* The address bar keeps the choice, so a reload comes back the
				 * way it was left. */
				var param = root.getAttribute('data-hide-param') || 'hide';
				try {
					var url = new URL(location.href);
					var hidden = url.searchParams.getAll(param).filter(function (one) { return one !== key; });
					if (!on) hidden.push(key);
					url.searchParams.delete(param);
					hidden.sort().forEach(function (one) { url.searchParams.append(param, one); });
					history.replaceState(history.state, '', url.pathname + url.search + url.hash);
				} catch (e) { /* an address bar that will not be written stays as it is */ }
				forget();
				show();
			};

			ctx.change = function (event) {
				var master = event.target.closest('[data-select-all]');
				if (master) {
					var held = selectionOf(root);
					visibleBoxes(root).forEach(function (box) {
						if (master.checked) held.keys[box.value] = true;
						else delete held.keys[box.value];
					});
					syncSelection(root);
					return;
				}
				var box = event.target.closest('[data-part="select"]');
				if (box) {
					var keys = selectionOf(root).keys;
					if (box.checked) keys[box.value] = true;
					else delete keys[box.value];
					syncSelection(root);
					return;
				}
				if (!complete) return;
				var filter = event.target.closest('[data-part="filter"]');
				if (filter) {
					facetSummary(filter.closest('[data-part="facet"]'));
					page = 1;
					show();
				}
			};

			ctx.key = function (event) {
				if (root.getAttribute('data-navigable') !== 'true') return;
				var cell = event.target.closest('td, th');
				if (!cell || !root.contains(cell)) return;

				var row = cell.parentElement;
				var rows = Array.prototype.slice.call(root.querySelectorAll('tr'));
				var at = Array.prototype.indexOf.call(row.children, cell);
				var down = rows.indexOf(row);

				var go = function (r, c) {
					event.preventDefault();
					var target = rows[r] && rows[r].children[c];
					if (!target) return;
					rows.forEach(function (one) {
						Array.prototype.forEach.call(one.children, function (child) {
							child.setAttribute('tabindex', '-1');
						});
					});
					target.setAttribute('tabindex', '0');
					target.focus();
				};

				/* Read once, at the gesture: in a right-to-left document the
				 * arrow that points right moves towards the earlier column. */
				var rtl = getComputedStyle(root).direction === 'rtl';
				var forward = rtl ? -1 : 1;

				switch (event.key) {
					case 'ArrowRight': go(down, at + forward); break;
					case 'ArrowLeft': go(down, at - forward); break;
					case 'ArrowDown': go(down + 1, at); break;
					case 'ArrowUp': go(down - 1, at); break;
					case 'Home': go(down, 0); break;
					case 'End': go(down, row.children.length - 1); break;
				}
			};

			/* A navigable table needs one cell reachable by tab, and the server
			 * cannot know which -- it is wherever the person left off. */
			if (root.getAttribute('data-navigable') === 'true') {
				each(root, 'td, th', function (cell) { cell.setAttribute('tabindex', '-1'); });
				var first = root.querySelector('th, td');
				if (first) first.setAttribute('tabindex', '0');
			}

			root.addEventListener('change', ctx.change);
			root.addEventListener('keydown', ctx.key);

			if (complete) {
				/* The links stay links. Taking the click in the capture phase,
				 * before htmx's own handler on the link and before the browser
				 * follows it, is what makes this an enhancement: with no script
				 * the same address loads the same page from the server. */
				ctx.click = function (event) {
					var from = origin(event);
					if (!from) return;
					var take = function () { event.preventDefault(); event.stopPropagation(); };

					var link = from.closest('[data-part="sort"]');
					if (link) {
						take();
						var head = link.closest('th');
						sortBy(head, head.getAttribute('aria-sort') === 'ascending' ? 'desc' : 'asc');
						return;
					}
					var item = from.closest('[data-sort-key][data-dir]');
					if (item) {
						take();
						sortBy(item.closest('th'), item.getAttribute('data-dir'));
						closeMenus(null);
						return;
					}
					var chosen = from.closest('[data-size]');
					if (chosen) {
						take();
						size = Number(chosen.getAttribute('data-size')) || 0;
						each(root, '[data-size]', function (item) {
							var on = item === chosen;
							item.setAttribute('aria-checked', on ? 'true' : 'false');
							each(item, '[data-indicator]', function (check) { check.hidden = !on; });
						});
						each(root, '[data-size-value]', function (value) { value.textContent = String(size); });
						page = 1;
						show();
						return;
					}
					var step = from.closest('[data-step]');
					if (step) {
						take();
						if (step.getAttribute('aria-disabled') === 'true') return;
						var pages = size > 0 ? Math.max(1, Math.ceil(Number(root.getAttribute('data-total') || 0) / size)) : 1;
						var to = step.getAttribute('data-step');
						page = to === 'first' ? 1 : to === 'last' ? pages : to === 'next' ? page + 1 : page - 1;
						show();
						return;
					}
					var toggle = from.closest('[data-column-toggle]');
					if (toggle) {
						take();
						hideColumn(toggle.getAttribute('data-column-toggle'), toggle.getAttribute('aria-checked') !== 'true');
						return;
					}
					var hide = from.closest('[data-column-hide]');
					if (hide) {
						take();
						hideColumn(hide.getAttribute('data-column-hide'), false);
						closeMenus(null);
						return;
					}
					if (from.closest('[data-part="reset"], [data-facet-clear-link]')) {
						take();
						var scope = from.closest('[data-facet-clear-link]') ? from.closest('[data-part="facet"]') : root;
						if (scope === root) {
							each(root, '[data-part="search"]', function (box) { box.value = ''; });
						}
						each(scope, '[data-part="filter"]', function (box) { box.checked = false; });
						each(root, '[data-part="facet"]', facetSummary);
						page = 1;
						show();
					}
				};
				root.addEventListener('click', ctx.click, true);

				/* Debounced for the same reason the server search is: a word
				 * typed at speed is one answer and not eight. Shorter, because
				 * there is no network to wait for. */
				var pending = 0;
				ctx.filter = function (event) {
					if (!event.target.closest('[data-part="search"]')) return;
					window.clearTimeout(pending);
					pending = window.setTimeout(function () { page = 1; show(); }, 80);
				};
				root.addEventListener('input', ctx.filter);

				ctx.refresh = function () { forget(); show(); };
				nameHeads();
				each(root, '[data-part="facet"]', facetSummary);
				show();
			} else {
				syncSelection(root);
			}
		},
		updated: function (ctx) {
			/* The body may be entirely different rows now. What each was
			 * searched by is the first thing that is no longer true. */
			if (ctx.refresh) ctx.refresh();
			else syncSelection(ctx.element);
		},
		destroyed: function (ctx) {
			var root = ctx.element;
			root.removeEventListener('change', ctx.change);
			root.removeEventListener('keydown', ctx.key);
			if (ctx.click) root.removeEventListener('click', ctx.click, true);
			if (ctx.filter) root.removeEventListener('input', ctx.filter);
			window.clearTimeout(tableTimers[root.id]);
			/* The selection belongs to the list and not to its markup. It
			 * survives the region being swapped for the same region; it ends
			 * when the page leaves the list. */
			if (root.id && !tableSwapping[root.id]) delete tableSelections[root.id];
		},
	});

	/* ---- dialog ---------------------------------------------------------------
	 *
	 * Opening and closing a <dialog> from an attribute, so nothing has to write
	 * an inline handler to do it.
	 *
	 * The components' own documentation used to say `onclick="ID.showModal()"`,
	 * and that is advice this project's policy refuses: the CSP is
	 * script-src 'self' with no 'unsafe-inline', which blocks an inline handler
	 * exactly as it blocks an inline <script>. The advice would work on a page
	 * that had loosened the policy, and on no page that had not -- which is the
	 * worst kind of wrong, because it works while somebody is building and
	 * fails when the headers go on.
	 *
	 * So it is a delegated attribute, like every other behaviour here: a name
	 * that is looked up, never a string that is evaluated.
	 *
	 *     <button data-dialog-open="confirm-delete">Delete</button>
	 *     <button data-dialog-close>Cancel</button>
	 *
	 * showModal rather than show, because a modal is what these panels are:
	 * the rest of the document goes inert, focus is trapped, and Escape closes
	 * -- all three from the element, none of them written here.
	 *
	 * # A trigger that is a link
	 *
	 * The trigger may be an <a href> to the page that holds the same form, which
	 * is what a form dialog is opened from: without this file the link goes to
	 * that page, and the form is posted and refused there like any other. With
	 * it, an ordinary click opens the dialog instead. A click that asks for a
	 * new tab or a new window -- a modifier held, or a button other than the
	 * first -- is left to the browser, because that person asked for the page.
	 *
	 * # Where focus goes when it closes
	 *
	 * Back to whatever opened it. The element does that on its own when the
	 * opener had focus, and an opener that was clicked does not always have it:
	 * Safari focuses neither a link nor a button on a click. Focus then lands on
	 * the body, and somebody on a keyboard starts again from the top of the
	 * page. So the opener is remembered, and given focus back when the element
	 * left it nowhere. A dialog nobody clicked open -- one reopened after a
	 * refused submit -- returns focus to the first trigger on the page that
	 * names it.
	 */
	var openers = new WeakMap();

	function opensInPlace(event, trigger) {
		if (trigger.tagName !== 'A') return true;
		return !(event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey);
	}

	function triggerOf(panel) {
		if (!panel.id) return null;
		var name = window.CSS && CSS.escape ? CSS.escape(panel.id) : panel.id;
		return document.querySelector('[data-dialog-open="' + name + '"]');
	}

	/* showModal on an element that is already open, but not as a modal, throws.
	 * That is the state a dialog is drawn in when it has to be readable without
	 * this file -- see form-dialog below -- so it is shut first and opened
	 * again the way every dialog here is opened. */
	function showModal(panel) {
		var modal = false;
		try { modal = panel.matches(':modal'); } catch (e) { modal = false; }
		if (panel.open && modal) return;
		if (panel.open) panel.close();
		panel.showModal();
		landOnAControl(panel);
	}

	/* The element puts focus on the first thing in the panel that can take it,
	 * and a region that scrolls can: a form longer than the screen sends the
	 * cursor to its scrolling body, where typing does nothing. A panel that
	 * chose a control -- autofocus, or a first field -- is left as it is. */
	var CONTROL = 'input:not([type="hidden"]), select, textarea, button, a[href], [contenteditable], [tabindex]:not([tabindex="-1"])';

	function landOnAControl(panel) {
		var focused = document.activeElement;
		if (!focused || !panel.contains(focused) || focused.matches(CONTROL)) return;
		var first = panel.querySelector('input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled])');
		if (first) first.focus();
	}

	document.addEventListener('click', function (event) {
		var open = event.target.closest('[data-dialog-open]');
		if (open) {
			var panel = document.getElementById(open.getAttribute('data-dialog-open'));
			/* A dialog that is not on the page is a page that changed under a
			 * button, and it is worth one line rather than a silent nothing. A
			 * link is left to go where it points. */
			if (!panel || typeof panel.showModal !== 'function') {
				miss('dialog', open.getAttribute('data-dialog-open'));
				return;
			}
			if (!opensInPlace(event, open)) return;
			event.preventDefault();
			openers.set(panel, open);
			showModal(panel);
			return;
		}

		var close = event.target.closest('[data-dialog-close]');
		if (!close) return;
		/* The named dialog, or the one this button is inside -- so a close
		 * button needs no id when it sits in the panel it closes. */
		var named = close.getAttribute('data-dialog-close');
		var target = named ? document.getElementById(named) : close.closest('dialog');
		if (!target) return;
		event.preventDefault();
		target.close();
	});

	/* close does not bubble, so this listens in the capture phase. A dialog
	 * that is open again by the time the event arrives was shut only to be
	 * reopened as a modal, and focus is already where it belongs. */
	document.addEventListener('close', function (event) {
		var panel = origin(event);
		if (!panel || panel.tagName !== 'DIALOG' || panel.open) return;
		var opener = openers.get(panel) || triggerOf(panel);
		openers.delete(panel);
		if (!opener || !opener.isConnected || typeof opener.focus !== 'function') return;
		var active = document.activeElement;
		if (!active || active === document.body || panel.contains(active)) opener.focus();
	}, true);

	/* ---- form dialog -----------------------------------------------------------
	 *
	 * A dialog holding a form, which has to open by itself on one occasion: the
	 * form inside it was refused. The server answers a refused form by sending
	 * the person back to the page they posted from, with the messages and what
	 * they typed, and the page they posted from is the list the dialog sits on.
	 * Drawn as it was before, the messages would be inside a closed dialog.
	 *
	 * So the component draws it with props {"open": true} when the page says
	 * the refused form was this one, and this opens it as a modal and puts the
	 * cursor on the first field that was refused. Opening it is the only thing
	 * this does: the messages, the typed values and the field that is invalid
	 * are all in the markup the server drew.
	 *
	 * The component also writes the open attribute in that case, so the form is
	 * readable on a page where this file never ran -- shown in place rather than
	 * as a modal. showModal above turns that into the modal.
	 */
	arandu.ui.define('form-dialog', {
		mounted: function (ctx) {
			var panel = ctx.element;
			if (!ctx.props || ctx.props.open !== true) return;
			if (typeof panel.showModal !== 'function') return;
			showModal(panel);
			var refused = panel.querySelector('[aria-invalid="true"]');
			if (refused && typeof refused.focus === 'function') refused.focus();
		},
	});

	/* ---- calendar ------------------------------------------------------------
	 *
	 * A month grid: the arrow keys move a day at a time, the buttons move a
	 * month, and choosing writes the date into the input the calendar was
	 * pointed at.
	 *
	 * # The arithmetic is here and the words are not
	 *
	 * Moving to another month redraws the grid rather than asking the server,
	 * so a month change costs nothing. That is a second copy of the date
	 * arithmetic the component already did in Go, and it is worth saying so
	 * plainly rather than pretending otherwise -- what keeps the two from
	 * drifting is that neither decides anything a person reads. The month
	 * names, the column heads, the first day of the week and the bounds all
	 * arrive as props, drawn from the application's catalogue. Nothing here
	 * formats a name, reads a locale or picks a calendar system, so the grid
	 * this draws and the grid the server drew say the same words.
	 *
	 * # Why the cells are not buttons
	 *
	 * A month is forty-two cells. As buttons that is forty-two tab stops, and
	 * the grid role exists precisely so it is one: the arrows move within it,
	 * and tab leaves it. That is the contract a person already knows from
	 * every other calendar.
	 */
	arandu.ui.define('calendar', {
		mounted: function (ctx) {
			var root = ctx.element;
			var props = ctx.props || {};

			var pad = function (n) { return (n < 10 ? '0' : '') + n; };
			var iso = function (y, m, d) { return y + '-' + pad(m + 1) + '-' + pad(d); };

			var cells = function () {
				return Array.prototype.slice.call(root.querySelectorAll('[role="gridcell"]'));
			};
			var focusDate = function (date) {
				var all = cells();
				for (var at = 0; at < all.length; at++) {
					if (all[at].getAttribute('data-date') !== date) continue;
					all.forEach(function (one) { one.setAttribute('tabindex', '-1'); });
					all[at].setAttribute('tabindex', '0');
					all[at].focus();
					return true;
				}
				return false;
			};

			/* draw rewrites the grid for one month. It writes numbers, dates
			 * and state, and nothing else -- the elements are the ones the
			 * server drew, so every class and every part name is untouched. */
			var draw = function (year, month) {
				var first = new Date(Date.UTC(year, month, 1));
				var firstDay = Number(props.firstDay) || 0;
				var offset = (first.getUTCDay() - firstDay + 7) % 7;
				var start = new Date(Date.UTC(year, month, 1 - offset));

				var today = new Date();
				var todayISO = iso(today.getFullYear(), today.getMonth(), today.getDate());
				var chosen = root.getAttribute('data-value') || '';

				cells().forEach(function (cell, at) {
					var day = new Date(start.getTime());
					day.setUTCDate(day.getUTCDate() + at);
					var date = iso(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate());

					cell.textContent = String(day.getUTCDate());
					cell.setAttribute('data-date', date);
					attr(cell, 'data-outside', day.getUTCMonth() !== month ? 'true' : null);
					attr(cell, 'data-today', date === todayISO ? 'true' : null);
					cell.setAttribute('aria-selected', date === chosen ? 'true' : 'false');
					attr(cell, 'aria-disabled', outside(date) ? 'true' : null);
					cell.setAttribute('tabindex', '-1');
				});

				root.setAttribute('data-month', year + '-' + pad(month + 1));
				var title = root.querySelector('[data-part="title"]');
				var names = props.months || [];
				if (title && names.length === 12) title.textContent = names[month] + ' ' + year;
			};

			var attr = function (element, name, value) {
				if (value === null) element.removeAttribute(name);
				else element.setAttribute(name, value);
			};

			var outside = function (date) {
				if (props.min && date < props.min) return true;
				return !!(props.max && date > props.max);
			};

			var month = function () {
				var parts = (root.getAttribute('data-month') || '').split('-');
				return { year: Number(parts[0]), month: Number(parts[1]) - 1 };
			};

			var step = function (by) {
				var at = month();
				var moved = new Date(Date.UTC(at.year, at.month + by, 1));
				draw(moved.getUTCFullYear(), moved.getUTCMonth());
			};

			/* choose writes the day into the input the calendar points at, and
			 * fires the events a form and htmx are listening for -- setting
			 * .value fires neither on its own. */
			var choose = function (date) {
				root.setAttribute('data-value', date || '');
				cells().forEach(function (cell) {
					cell.setAttribute('aria-selected',
						date && cell.getAttribute('data-date') === date ? 'true' : 'false');
				});
				if (!props.target) return;
				var field = document.getElementById(props.target);
				if (!field) return;
				field.value = date || '';
				field.dispatchEvent(new Event('input', { bubbles: true }));
				field.dispatchEvent(new Event('change', { bubbles: true }));
			};

			ctx.click = function (event) {
				var stepper = event.target.closest('[data-calendar-step]');
				if (stepper) { step(Number(stepper.getAttribute('data-calendar-step')) || 1); return; }
				if (event.target.closest('[data-calendar-clear]')) { choose(''); return; }
				if (event.target.closest('[data-calendar-today]')) {
					var now = new Date();
					draw(now.getFullYear(), now.getMonth());
					var date = iso(now.getFullYear(), now.getMonth(), now.getDate());
					if (!outside(date)) { choose(date); focusDate(date); }
					return;
				}
				var cell = event.target.closest('[role="gridcell"]');
				if (cell && cell.getAttribute('aria-disabled') !== 'true') {
					choose(cell.getAttribute('data-date'));
					focusDate(cell.getAttribute('data-date'));
				}
			};

			ctx.key = function (event) {
				var cell = event.target.closest('[role="gridcell"]');
				if (!cell) return;
				var parts = cell.getAttribute('data-date').split('-');
				var day = new Date(Date.UTC(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2])));

				var move = function (days) {
					event.preventDefault();
					day.setUTCDate(day.getUTCDate() + days);
					var date = iso(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate());
					if (!focusDate(date)) {
						draw(day.getUTCFullYear(), day.getUTCMonth());
						focusDate(date);
					}
				};

				switch (event.key) {
					case 'ArrowRight': move(1); break;
					case 'ArrowLeft': move(-1); break;
					case 'ArrowDown': move(7); break;
					case 'ArrowUp': move(-7); break;
					/* Home and End are the ends of the week, which is the row
					 * the person is looking at -- not the ends of the month,
					 * which PageUp and PageDown reach. */
					case 'Home': move(-((day.getUTCDay() - (Number(props.firstDay) || 0) + 7) % 7)); break;
					case 'End': move(6 - ((day.getUTCDay() - (Number(props.firstDay) || 0) + 7) % 7)); break;
					case 'PageUp': event.preventDefault(); step(-1); break;
					case 'PageDown': event.preventDefault(); step(1); break;
					case 'Enter':
					case ' ':
						event.preventDefault();
						if (cell.getAttribute('aria-disabled') !== 'true') choose(cell.getAttribute('data-date'));
						break;
				}
			};

			root.addEventListener('click', ctx.click);
			root.addEventListener('keydown', ctx.key);
		},
		destroyed: function (ctx) {
			ctx.element.removeEventListener('click', ctx.click);
			ctx.element.removeEventListener('keydown', ctx.key);
		},
	});

	/* ---- roving ---------------------------------------------------------------
	 *
	 * One tab stop for a group of controls, with the arrow keys moving inside
	 * it. A toolbar, a menubar and a tree all want exactly this, so it is
	 * written once.
	 *
	 * The tab stop moves rather than multiplying: whichever control is current
	 * has tabindex 0 and every other has -1, so tabbing out and back in
	 * returns to where the person was rather than to the beginning.
	 */
	function roving(container, selector, options) {
		var settings = options || {};
		var items = function () {
			return Array.prototype.filter.call(
				container.querySelectorAll(selector),
				function (element) {
					return !element.disabled &&
						element.getAttribute('aria-disabled') !== 'true' &&
						element.offsetParent !== null;
				}
			);
		};

		var focus = function (element) {
			if (!element) return;
			items().forEach(function (one) { one.setAttribute('tabindex', '-1'); });
			element.setAttribute('tabindex', '0');
			element.focus();
		};

		container.addEventListener('keydown', function (event) {
			var all = items();
			var at = all.indexOf(document.activeElement);
			if (at < 0) return;

			var vertical = settings.vertical;
			var forward = vertical ? 'ArrowDown' : 'ArrowRight';
			var backward = vertical ? 'ArrowUp' : 'ArrowLeft';

			if (event.key === forward) {
				event.preventDefault();
				focus(all[(at + 1) % all.length]);
			} else if (event.key === backward) {
				event.preventDefault();
				focus(all[(at - 1 + all.length) % all.length]);
			} else if (event.key === 'Home') {
				event.preventDefault();
				focus(all[0]);
			} else if (event.key === 'End') {
				event.preventDefault();
				focus(all[all.length - 1]);
			} else if (settings.onKey) {
				settings.onKey(event, all, at, focus);
			}
		});

		/* A click also moves the tab stop, so the two ways of getting to a
		 * control agree about where the keyboard is. */
		container.addEventListener('click', function (event) {
			var hit = event.target.closest(selector);
			if (hit && items().indexOf(hit) >= 0) {
				items().forEach(function (one) { one.setAttribute('tabindex', '-1'); });
				hit.setAttribute('tabindex', '0');
			}
		});
	}

	/* ---- toolbar -------------------------------------------------------------
	 *
	 * A row of controls that is one tab stop. Nothing else: the controls do
	 * what they do, and this only decides which of them the keyboard is on.
	 */
	arandu.ui.define('toolbar', {
		mounted: function (ctx) {
			roving(ctx.element, '[data-part="control"]', {
				vertical: !!(ctx.props || {}).vertical,
			});
		},
	});

	/* ---- menubar -------------------------------------------------------------
	 *
	 * The row of menus along the top of an application.
	 *
	 * Moving sideways while a menu is open closes it and opens the next --
	 * that is the behaviour that makes a menubar quick, and it is the part
	 * most reimplementations leave out. Down opens and steps in; Escape closes
	 * and comes back to the trigger, which is where the person was.
	 */
	arandu.ui.define('menubar', {
		mounted: function (ctx) {
			var bar = ctx.element;

			var panelOf = function (trigger) {
				return document.getElementById(trigger.id.replace('-trigger-', '-panel-'));
			};
			var close = function (trigger) {
				var panel = panelOf(trigger);
				if (!panel) return;
				panel.setAttribute('aria-hidden', 'true');
				trigger.setAttribute('aria-expanded', 'false');
			};
			var closeAll = function () {
				bar.querySelectorAll('[data-part="trigger"]').forEach(close);
			};
			var open = function (trigger) {
				var panel = panelOf(trigger);
				if (!panel) return;
				closeAll();
				panel.setAttribute('aria-hidden', 'false');
				trigger.setAttribute('aria-expanded', 'true');
			};
			var isOpen = function (trigger) {
				return trigger.getAttribute('aria-expanded') === 'true';
			};

			roving(bar, '[data-part="trigger"]', {
				onKey: function (event, all, at) {
					if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
						event.preventDefault();
						open(all[at]);
						var first = panelOf(all[at]).querySelector('[role="menuitem"]:not([disabled])');
						if (first) first.focus();
					} else if (event.key === 'Escape') {
						closeAll();
					}
				},
			});

			/* The roving handler already moved the focus by the time this runs,
			 * so an open menu simply follows it to whatever is now focused. */
			bar.addEventListener('keyup', function (event) {
				if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
				var landed = document.activeElement;
				if (landed && landed.matches('[data-part="trigger"]') &&
					bar.querySelector('[data-part="trigger"][aria-expanded="true"]')) {
					open(landed);
				}
			});

			bar.addEventListener('click', function (event) {
				var trigger = event.target.closest('[data-part="trigger"]');
				if (!trigger) return;
				if (isOpen(trigger)) close(trigger); else open(trigger);
			});

			/* Inside a menu: up and down between entries, Escape back out to
			 * the trigger -- which is where the person came from and where
			 * they expect to be. */
			bar.addEventListener('keydown', function (event) {
				var entry = event.target.closest('[role="menuitem"]');
				if (!entry) return;
				var menu = entry.closest('[role="menu"]');
				var entries = Array.prototype.filter.call(
					menu.querySelectorAll('[role="menuitem"]'),
					function (one) { return !one.disabled; }
				);
				var at = entries.indexOf(entry);
				var trigger = document.getElementById(menu.getAttribute('aria-labelledby'));

				if (event.key === 'ArrowDown') {
					event.preventDefault();
					entries[(at + 1) % entries.length].focus();
				} else if (event.key === 'ArrowUp') {
					event.preventDefault();
					entries[(at - 1 + entries.length) % entries.length].focus();
				} else if (event.key === 'Escape') {
					event.preventDefault();
					close(trigger);
					trigger.focus();
				}
			});

			ctx.dismiss = function (event) {
				if (!bar.contains(event.target)) closeAll();
			};
			document.addEventListener('click', ctx.dismiss);
		},
		destroyed: function (ctx) {
			if (ctx.dismiss) document.removeEventListener('click', ctx.dismiss);
		},
	});

	/* ---- tree ----------------------------------------------------------------
	 *
	 * A hierarchy that is one tab stop.
	 *
	 * Right opens a closed branch and then steps into it; left closes an open
	 * one and then steps out to the parent. That two-stage behaviour is the
	 * whole tree contract and is what makes a deep tree navigable with two
	 * keys.
	 *
	 * Opening and closing is done here rather than fetched, because the rows
	 * are already in the markup -- a tree whose branches are fetched swaps its
	 * own rows and this behaviour re-mounts on what comes back.
	 */
	arandu.ui.define('tree', {
		mounted: function (ctx) {
			var tree = ctx.element;
			var multiple = !!(ctx.props || {}).multiple;

			var rows = function () {
				return Array.prototype.filter.call(
					tree.querySelectorAll('[role="treeitem"]'),
					function (row) { return row.getAttribute('aria-disabled') !== 'true'; }
				);
			};
			var levelOf = function (row) {
				return Number(row.getAttribute('aria-level') || '1');
			};
			var move = function (row) {
				if (!row) return;
				rows().forEach(function (one) { one.setAttribute('tabindex', '-1'); });
				row.setAttribute('tabindex', '0');
				row.focus();
			};
			var toggle = function (row, open) {
				if (!row.hasAttribute('aria-expanded')) return false;
				if (row.getAttribute('aria-expanded') === String(open)) return false;
				row.setAttribute('aria-expanded', String(open));
				/* The children are the following rows deeper than this one, up
				 * to the next row at the same level or shallower. */
				var all = Array.prototype.slice.call(tree.querySelectorAll('[role="treeitem"]'));
				var at = all.indexOf(row);
				var depth = levelOf(row);
				for (var next = at + 1; next < all.length; next++) {
					if (levelOf(all[next]) <= depth) break;
					all[next].hidden = !open || levelOf(all[next]) > depth + 1
						? !open || all[next].closest('[aria-expanded="false"]') !== null
						: !open;
				}
				return true;
			};

			tree.addEventListener('keydown', function (event) {
				var row = event.target.closest('[role="treeitem"]');
				if (!row) return;
				var all = rows();
				var at = all.indexOf(row);

				if (event.key === 'ArrowDown') {
					event.preventDefault();
					move(all[Math.min(at + 1, all.length - 1)]);
				} else if (event.key === 'ArrowUp') {
					event.preventDefault();
					move(all[Math.max(at - 1, 0)]);
				} else if (event.key === 'ArrowRight') {
					event.preventDefault();
					if (!toggle(row, true) && at + 1 < all.length &&
						levelOf(all[at + 1]) > levelOf(row)) {
						move(all[at + 1]);
					}
				} else if (event.key === 'ArrowLeft') {
					event.preventDefault();
					if (!toggle(row, false)) {
						for (var back = at - 1; back >= 0; back--) {
							if (levelOf(all[back]) < levelOf(row)) { move(all[back]); break; }
						}
					}
				} else if (event.key === 'Home') {
					event.preventDefault();
					move(all[0]);
				} else if (event.key === 'End') {
					event.preventDefault();
					move(all[all.length - 1]);
				} else if (event.key === 'Enter' || event.key === ' ') {
					if (!multiple) {
						all.forEach(function (one) { one.removeAttribute('aria-selected'); });
					}
					row.setAttribute('aria-selected',
						row.getAttribute('aria-selected') === 'true' && multiple ? 'false' : 'true');
				}
			});

			tree.addEventListener('click', function (event) {
				var toggleHit = event.target.closest('[data-part="toggle"]');
				var row = event.target.closest('[role="treeitem"]');
				if (!row) return;
				move(row);
				if (toggleHit) {
					toggle(row, row.getAttribute('aria-expanded') !== 'true');
				}
			});
		},
	});

	/* ---- carousel ------------------------------------------------------------
	 *
	 * The two buttons beside a scroll-snapping row.
	 *
	 * The scrolling itself belongs to the browser -- a drag, a swipe and the
	 * scrollbar all already work -- so this only asks it to scroll by one
	 * slide. That is why a carousel with no script still works and this only
	 * adds a way to do it with a pointer that is not dragging.
	 */
	arandu.ui.define('carousel', {
		mounted: function (ctx) {
			var root = ctx.element;
			var viewport = root.querySelector('[data-part="viewport"]');
			if (!viewport) return;
			var vertical = !!(ctx.props || {}).vertical;

			ctx.step = function (event) {
				var button = event.target.closest('[data-carousel-step]');
				if (!button) return;
				var direction = Number(button.getAttribute('data-carousel-step')) || 1;
				var slide = viewport.querySelector('[data-part="slide"]');
				var by = slide
					? (vertical ? slide.offsetHeight : slide.offsetWidth)
					: (vertical ? viewport.clientHeight : viewport.clientWidth);
				var to = {};
				to[vertical ? 'top' : 'left'] = by * direction;
				to.behavior = 'smooth';
				viewport.scrollBy(to);
			};
			root.addEventListener('click', ctx.step);
		},
		destroyed: function (ctx) {
			if (ctx.step) ctx.element.removeEventListener('click', ctx.step);
		},
	});

	/* ---- file-upload ---------------------------------------------------------
	 *
	 * Dropping files into the real file input.
	 *
	 * The input is what holds them, always: a drop writes into input.files
	 * through a DataTransfer, so the form submits exactly as if the files had
	 * been picked. Nothing here keeps a list of its own, which is what would
	 * otherwise disagree with the field the moment somebody picks again.
	 *
	 * dragover has to be cancelled or the browser opens the file instead, and
	 * that is the one line without which nothing works.
	 */
	arandu.ui.define('file-upload', {
		mounted: function (ctx) {
			var root = ctx.element;
			var zone = root.querySelector('[data-dropzone]');
			var input = root.querySelector('[data-part="input"]');
			if (!zone || !input) return;

			var stop = function (event) {
				event.preventDefault();
				event.stopPropagation();
			};

			ctx.over = function (event) {
				stop(event);
				zone.setAttribute('data-dragging', 'true');
			};
			ctx.leave = function (event) {
				stop(event);
				zone.removeAttribute('data-dragging');
			};
			ctx.drop = function (event) {
				stop(event);
				zone.removeAttribute('data-dragging');
				if (!event.dataTransfer || !event.dataTransfer.files.length) return;

				var carried = new DataTransfer();
				var files = event.dataTransfer.files;
				var many = input.multiple;
				for (var at = 0; at < files.length; at++) {
					carried.items.add(files[at]);
					if (!many) break;
				}
				input.files = carried.files;
				/* change, not input: it is the event a file input fires when
				 * files are picked, and it is what the hx-trigger listens for. */
				input.dispatchEvent(new Event('change', { bubbles: true }));
			};

			zone.addEventListener('dragenter', ctx.over);
			zone.addEventListener('dragover', ctx.over);
			zone.addEventListener('dragleave', ctx.leave);
			zone.addEventListener('drop', ctx.drop);
		},
		destroyed: function (ctx) {
			var zone = ctx.element.querySelector('[data-dropzone]');
			if (!zone) return;
			zone.removeEventListener('dragenter', ctx.over);
			zone.removeEventListener('dragover', ctx.over);
			zone.removeEventListener('dragleave', ctx.leave);
			zone.removeEventListener('drop', ctx.drop);
		},
	});

	/* ---- optimistic-toggle ---------------------------------------------------
	 *
	 * Flip now, reconcile after.
	 *
	 * The flip happens on the press and the request goes out behind it. What
	 * the server answers with is this control redrawn in the state it actually
	 * stored, which replaces the optimistic one -- so a disagreement corrects
	 * itself without anything here comparing the two.
	 *
	 * A failed request is the case that needs handling: nothing comes back to
	 * replace the control, so the flip is undone here and the count with it.
	 */
	arandu.ui.define('optimistic-toggle', {
		mounted: function (ctx) {
			var button = ctx.element;

			var paint = function (pressed) {
				button.setAttribute('aria-pressed', pressed ? 'true' : 'false');
				var on = button.querySelector('[data-toggle-on]');
				var off = button.querySelector('[data-toggle-off]');
				if (on) on.hidden = !pressed;
				if (off) off.hidden = pressed;
			};

			ctx.flip = function () {
				ctx.was = button.getAttribute('aria-pressed') === 'true';
				var now = !ctx.was;
				paint(now);

				var count = button.querySelector('[data-toggle-count]');
				if (count) {
					var value = parseInt(count.textContent, 10);
					if (!isNaN(value)) {
						ctx.count = count.textContent;
						count.textContent = String(Math.max(0, value + (now ? 1 : -1)));
					}
				}
			};

			ctx.revert = function () {
				if (ctx.was === undefined) return;
				paint(ctx.was);
				var count = button.querySelector('[data-toggle-count]');
				if (count && ctx.count !== undefined) count.textContent = ctx.count;
			};

			button.addEventListener('click', ctx.flip);
			/* htmx:responseError covers a 4xx or 5xx; htmx:sendError covers the
			 * request never arriving. Both leave the control as it was drawn,
			 * and both have to put it back. */
			button.addEventListener('htmx:responseError', ctx.revert);
			button.addEventListener('htmx:sendError', ctx.revert);
		},
		destroyed: function (ctx) {
			ctx.element.removeEventListener('click', ctx.flip);
			ctx.element.removeEventListener('htmx:responseError', ctx.revert);
			ctx.element.removeEventListener('htmx:sendError', ctx.revert);
		},
	});

	/* ---- number-input --------------------------------------------------------
	 *
	 * The two buttons beside a number box.
	 *
	 * The stepping itself is the browser's -- stepUp and stepDown honour min,
	 * max and step, and clamp rather than overshoot -- so this file decides
	 * nothing about arithmetic. What it adds is the press, and the input event
	 * afterwards, because stepUp does not fire one and everything watching the
	 * box for changes would otherwise never hear about these.
	 *
	 * The buttons are aria-hidden and out of the tab order in the markup: a
	 * keyboard already steps this box with the arrow keys, and two more tab
	 * stops per number on a form is a form nobody can get through.
	 */
	arandu.ui.define('number-input', {
		mounted: function (ctx) {
			var root = ctx.element;
			var input = root.querySelector('[data-part="input"]');
			if (!input) return;

			ctx.step = function (event) {
				var button = event.target.closest('[data-number-step]');
				if (!button || button.disabled || input.disabled || input.readOnly) return;
				var direction = Number(button.getAttribute('data-number-step'));
				if (!direction) return;
				try {
					if (direction > 0) input.stepUp(); else input.stepDown();
				} catch (error) {
					/* stepUp throws on a box whose value is not a number the
					 * element can read -- a half-typed exponent, an empty box in
					 * some engines. Starting from zero is the answer a person
					 * pressing "up" on an empty box expects. */
					input.value = String(direction > 0 ? 1 : -1);
				}
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
			};
			root.addEventListener('click', ctx.step);
		},
		destroyed: function (ctx) {
			if (ctx.step) ctx.element.removeEventListener('click', ctx.step);
		},
	});

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

	/* ---- sidebar -----------------------------------------------------------
	 *
	 * A button whose data-sidebar-toggle names a sidebar's id folds it. On a
	 * wide screen a sidebar the server drew with a mode -- data-sidebar-mode,
	 * "full" or "slim" -- switches to the other one, and the choice is kept on
	 * the device, so the next page comes back the way the last was left. On a
	 * narrow screen, or for a sidebar drawn with no mode, the column is the
	 * overlay and the button opens or closes it.
	 *
	 * The mode the server drew is right on the first paint with no script and
	 * no storage, and it stays the answer whenever storage throws -- a private
	 * window, blocked site data -- or holds anything but the two names. The
	 * stylesheet follows the attribute, so switching it is all there is to do.
	 *
	 * The width the column turns into an overlay at is the sidebar's own
	 * data-breakpoint, the one the vendored script reads, and 768 pixels
	 * without it: the two must agree on which screen is narrow, or a press
	 * would fold the strip while the overlay is what is on screen.
	 */

	var SIDEBAR_KEY = 'arandu.sidebar.';

	function sidebarMode(value) {
		return value === 'full' || value === 'slim' ? value : '';
	}

	function sidebarWide(sidebar) {
		var breakpoint = parseInt(sidebar.getAttribute('data-breakpoint'), 10) || 768;
		return window.innerWidth >= breakpoint;
	}

	/* Whether the sidebar is switching modes rather than opening and closing:
	 * a wide screen, and a mode the server drew. */
	function sidebarSwitches(sidebar) {
		return sidebarWide(sidebar) && sidebarMode(sidebar.getAttribute('data-sidebar-mode')) !== '';
	}

	/* aria-expanded on every button that names the sidebar: the full column
	 * is the expanded one, and the overlay is expanded while it is shown. */
	function stampSidebarToggles(sidebar) {
		var expanded = sidebarSwitches(sidebar)
			? sidebar.getAttribute('data-sidebar-mode') === 'full'
			: sidebar.getAttribute('aria-hidden') !== 'true';
		var toggles = document.querySelectorAll('[data-sidebar-toggle]');
		for (var i = 0; i < toggles.length; i++) {
			if (toggles[i].getAttribute('data-sidebar-toggle') === sidebar.id) {
				toggles[i].setAttribute('aria-expanded', expanded ? 'true' : 'false');
			}
		}
	}

	function toggleSidebar(button) {
		var id = button.getAttribute('data-sidebar-toggle');
		var sidebar = id ? document.getElementById(id) : null;
		if (!sidebar) {
			miss('sidebar', id);
			return;
		}
		if (sidebarSwitches(sidebar)) {
			var next = sidebar.getAttribute('data-sidebar-mode') === 'slim' ? 'full' : 'slim';
			sidebar.setAttribute('data-sidebar-mode', next);
			try {
				window.localStorage.setItem(SIDEBAR_KEY + id, next);
			} catch (e) {
				/* Not remembered: this page is switched all the same. */
			}
		} else if (sidebar.contains(button) && !sidebarWide(sidebar) &&
			!button.closest('[data-keep-mobile-sidebar-open]') &&
			sidebar.getAttribute('data-sidebar-initialized')) {
			/* The vendored script closes the overlay itself when a button
			 * inside it is pressed, before this listener hears the click;
			 * toggling here would open it again. */
		} else if (typeof sidebar.toggle === 'function') {
			sidebar.toggle();
		}
		stampSidebarToggles(sidebar);
	}

	/* The mode remembered on this device, put back on a sidebar that has just
	 * been drawn. Only a sidebar the server drew with a mode is touched, and
	 * only by one of the two names. */
	function restoreSidebar(sidebar) {
		if (!sidebar.id) return;
		var saved = '';
		try {
			saved = sidebarMode(window.localStorage.getItem(SIDEBAR_KEY + sidebar.id));
		} catch (e) {
			/* Nothing remembered: the server's mode stands. */
		}
		if (saved) sidebar.setAttribute('data-sidebar-mode', saved);
		if (sidebarSwitches(sidebar)) stampSidebarToggles(sidebar);
	}

	/* ---- the sweep ---------------------------------------------------------
	 *
	 * Three things a server cannot write and delegation cannot reach, because
	 * they are state rather than events. Which accent is in force lives in the
	 * browser, so no cached page can carry aria-current on the right button;
	 * the vendored stylesheet paints a hover highlight of its own until a
	 * component says it is driving one, which is what the initialised marker
	 * says; and the sidebar mode a person chose is remembered on the device,
	 * not on the server that drew the page. All three are idempotent, and none
	 * gates any behaviour.
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
		each(node, '[data-sidebar-mode]', restoreSidebar);
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
	 * It is the same shape as every delegated attribute above -- data-copy,
	 * data-dialog-open, data-slider: a catalogue rather than a parser is what
	 * keeps an attribute data instead of code.
	 */


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

		var sidebarToggle = from.closest('[data-sidebar-toggle]');
		if (sidebarToggle) toggleSidebar(sidebarToggle);

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
	 * Once this has mounted, the squares are the visible inputs and none of
	 * them submits; a hidden input beside them carries the whole code under the
	 * field's name, kept in step on every keystroke. So the server reads one
	 * field and never has to join six of them.
	 *
	 * # The squares are the enhancement
	 *
	 * The markup draws that input as an ordinary text field, labelled and
	 * named, with the squares hidden, so a form posts the code with no script
	 * at all. Mounting is what turns it into the hidden field: whatever was
	 * typed into it before this ran is spread across the squares, the squares
	 * are shown, and the first one takes the input's id, so a label still
	 * points at what takes the cursor -- and takes the cursor too, when the
	 * input had it or was drawn to get it. Markup that already draws
	 * the field hidden and the squares shown is mounted as it stands.
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

			if (raw && raw.type !== 'hidden') {
				/* Focus follows the field only when it had it, or was about to
				 * by autofocus with nothing else chosen yet: a cursor somebody
				 * already put elsewhere is not this behaviour's to move. */
				var active = document.activeElement;
				var focused = active === raw ||
					(raw.hasAttribute('autofocus') && (!active || active === document.body));
				var typed = String(raw.value).split('').filter(function (one) { return accepts.test(one); });
				if (raw.id && !squares[0].id) {
					squares[0].id = raw.id;
					raw.removeAttribute('id');
				}
				raw.removeAttribute('autofocus');
				raw.type = 'hidden';
				squares.forEach(function (one, at) {
					one.value = typed[at] || '';
					one.hidden = false;
				});
				if (focused) squares[Math.min(typed.length, squares.length - 1)].focus();
			}

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
	 * Once this has mounted, the visible field shows 123.456.789-00 and is not
	 * the field that submits. Beside it is a hidden input carrying 12345678900,
	 * kept in step on every keystroke, and that is what has the form name. So
	 * no query stores punctuation because somebody forgot to strip it in one of
	 * the places that read the field.
	 *
	 * The component writes both, so a caller cannot get half of it.
	 *
	 * # Until it mounts, the box is the field
	 *
	 * The markup draws the visible box carrying the form name and the hidden
	 * input disabled, so with no script the form posts what was typed, as it was
	 * typed, and the server unmasks it with the same pattern. Mounting hands the
	 * name over: the hidden input is enabled and filled from the format below,
	 * and the box takes the displayName the component wrote into the props, or
	 * no name at all when there is none. Markup whose hidden input already
	 * submits is mounted as it stands, and so is a box drawn disabled, which
	 * posts nothing either way.
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

			if (raw && raw.disabled && !input.disabled) {
				raw.disabled = false;
				if (typeof ctx.props.displayName === 'string' && ctx.props.displayName) {
					input.name = ctx.props.displayName;
				} else {
					input.removeAttribute('name');
				}
			}

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
				ctx.reveal = function () {
					var hidden = input.type === 'password';
					input.type = hidden ? 'text' : 'password';
					reveal.setAttribute('aria-pressed', hidden ? 'true' : 'false');
					/* The two names are in the markup, so this file carries no
					 * sentence and translates nothing. The control is an icon,
					 * so the name is the only thing a screen reader has: it has
					 * to say which state pressing lands in, and it flips with
					 * the picture rather than after it. */
					var name = reveal.getAttribute(hidden ? 'data-reveal-shown' : 'data-reveal-hidden');
					if (name) reveal.setAttribute('aria-label', name);
					toggle(reveal.querySelector('[data-reveal-icon-shown]'), hidden);
					toggle(reveal.querySelector('[data-reveal-icon-hidden]'), !hidden);
					/* A label written as text rather than as a picture is still
					 * supported: a caller who put the words inside the button
					 * gets them swapped the same way. */
					toggle(reveal.querySelector('[data-reveal-shown]'), hidden);
					toggle(reveal.querySelector('[data-reveal-hidden]'), !hidden);
				};
				reveal.addEventListener('click', ctx.reveal);
			}

			var panel = root.querySelector('[data-part="panel"]');
			var done = panel && panel.querySelector('[data-part="done"]');
			ctx.showPanel = function () { if (panel) { panel.hidden = false; input.setAttribute('aria-expanded', 'true'); } };
			ctx.closePanel = function () { if (panel) { panel.hidden = true; input.setAttribute('aria-expanded', 'false'); } };
			ctx.judge = function () { judge(root, input.value, ctx.props || {}); };
			ctx.typed = function () { ctx.judge(); ctx.showPanel(); };
			ctx.finish = function () { input.focus(); ctx.closePanel(); };
			ctx.escape = function (event) { if (event.key === 'Escape' && panel && !panel.hidden) { event.preventDefault(); ctx.finish(); } };
			ctx.left = function (event) { if (!root.contains(event.relatedTarget)) ctx.closePanel(); };
			input.addEventListener('input', ctx.typed);
			input.addEventListener('focus', ctx.showPanel);
			root.addEventListener('keydown', ctx.escape);
			root.addEventListener('focusout', ctx.left);
			if (done) done.addEventListener('click', ctx.finish);
			ctx.judge();
			ctx.closePanel();
		},
		destroyed: function (ctx) {
			var reveal = ctx.element.querySelector('[data-part="reveal"]');
			if (reveal && ctx.reveal) reveal.removeEventListener('click', ctx.reveal);
			var input = ctx.element.querySelector('[data-part="input"]');
			var root = ctx.element;
			if (input && ctx.typed) input.removeEventListener('input', ctx.typed);
			if (input && ctx.showPanel) input.removeEventListener('focus', ctx.showPanel);
			if (ctx.escape) root.removeEventListener('keydown', ctx.escape);
			if (ctx.left) root.removeEventListener('focusout', ctx.left);
			var done = root.querySelector('[data-part="panel"] [data-part="done"]');
			if (done && ctx.finish) done.removeEventListener('click', ctx.finish);
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
		var length = Array.from(value).length;
		var met = {
			min: typeof rules.min !== 'number' || length >= rules.min,
			max: typeof rules.max !== 'number' || rules.max <= 0 || length <= rules.max,
			letters: !rules.letters || /\p{L}/u.test(value),
			mixedCase: !rules.mixedCase || (/\p{Lu}/u.test(value) && /\p{Ll}/u.test(value)),
			numbers: !rules.numbers || /\p{N}/u.test(value),
			symbols: !rules.symbols || (typeof rules.symbolCharacters === 'string'
                ? Array.from(value).some(function (character) { return Array.from(rules.symbolCharacters).indexOf(character) !== -1; })
                : /[\p{P}\p{S}\p{White_Space}]/u.test(value)),
			/* Only the server knows, so this line never ticks here. */
			uncompromised: false,
		};

		var total = 0;
		var satisfied = 0;
		each(root, '[data-requirement]', function (line) {
			var key = line.getAttribute('data-requirement');
			var ok = met[key] === true;
            /* A maximum is a technical constraint, never a strength reward. */
            if (key === 'max') {
                line.hidden = !value || ok;
                line.classList.toggle('hidden', !value || ok);
            } else {
                total += 1;
            }
            if (!value) ok = false;
			if (ok && key !== 'max') satisfied += 1;
			line.setAttribute('data-met', ok ? 'true' : 'false');
			var status = line.querySelector('[data-password-status]');
			if (status) status.textContent = root.getAttribute(ok ? 'data-password-met' : 'data-password-unmet') || (ok ? 'Met:' : 'Not met:');
			var done = line.querySelector('[data-part="done"]');
			if (done) done.hidden = !ok;
		});

        var complete = !!value && total > 0 && satisfied === total && met.max;
        root.setAttribute('data-password-complete', complete ? 'true' : 'false');
        var fill = root.querySelector('[data-part="fill"]');
        if (fill) {
			fill.style.width = total === 0 ? '0%' : Math.round((satisfied / total) * 100) + '%';
		}
		var meter = root.querySelector('[data-part="meter"]');
		if (meter) {
			meter.setAttribute('data-password-complete', complete ? 'true' : 'false');
			meter.setAttribute('aria-valuenow', String(satisfied));
			meter.setAttribute('aria-valuemax', String(total));
		}
		/* An empty box has nothing to say about itself, so the panel that
		 * explains the policy stays open and the strength summary stays quiet. */
		var strength = root.querySelector('[data-part="strength"]');
		if (strength) {
			strength.setAttribute('data-met', complete ? 'true' : 'false');
			var label = strength.getAttribute('data-password-summary') || '{met} of {total} requirements met';
			strength.textContent = label.replace(/\{met\}/g, String(satisfied)).replace(/\{total\}/g, String(total));
		}
	}

	/* ---- copy ---------------------------------------------------------------
	 *
	 * Puts a value on the clipboard and says so.
	 *
	 * The confirmation is written into a live region inside the button rather
	 * than swapped for its label, because the label is what the control is
	 * called and a control that renames itself after being pressed is a
	 * control a screen reader can no longer find by name. The region says
	 * "Copied" and empties again; the button keeps its name throughout.
	 *
	 * The writing itself is the asynchronous clipboard, and it is allowed only
	 * from a gesture and only on a secure origin. Both are true here -- this
	 * runs inside a click -- but a page served over plain HTTP has no
	 * clipboard at all, so the failure is answered rather than assumed away:
	 * the region says the copy did not happen, which is the truth and is more
	 * use than a tick that lies.
	 */
	arandu.ui.define('copy', {
		mounted: function (ctx) {
			var button = ctx.element;
			ctx.copy = function () {
				var props = ctx.props || {};
				var text = props.value;
				if (!text && props.source) {
					var from = document.getElementById(props.source);
					if (from) text = from.value !== undefined ? from.value : from.textContent;
				}
				if (!text) return;

				var say = function (message) {
					var region = button.querySelector('[data-part="feedback"]');
					if (!region) return;
					region.textContent = message;
					/* Emptied again so the next copy is a change, and a change
					 * is the only thing a live region announces. */
					window.setTimeout(function () { region.textContent = ''; }, 2000);
				};

				if (!navigator.clipboard || !navigator.clipboard.writeText) {
					say('Copying is not available here');
					return;
				}
				navigator.clipboard.writeText(text).then(
					function () { say(props.copied || 'Copied'); },
					function () { say('Copy failed'); }
				);
			};
			button.addEventListener('click', ctx.copy);
		},
		destroyed: function (ctx) {
			if (ctx.copy) ctx.element.removeEventListener('click', ctx.copy);
		},
	});

	/* ---- relative-time -------------------------------------------------------
	 *
	 * Rewrites a timestamp in the reader's own locale and time zone.
	 *
	 * The server cannot know either: no request carries them, and a server
	 * that guesses from the address is wrong for everyone travelling. So the
	 * server writes the instant in the datetime attribute, where it is exact,
	 * and a sentence in the element, where it is readable -- and this replaces
	 * the sentence once, with the browser's own formatter.
	 *
	 * Everything here degrades to what the server wrote: an unparseable
	 * instant, a browser without Intl, a script that never ran. The element is
	 * never emptied and never left saying "Invalid Date".
	 */
	arandu.ui.define('relative-time', {
		mounted: function (ctx) {
			var element = ctx.element;
			var when = new Date(element.getAttribute('datetime'));
			if (isNaN(when.getTime())) return;

			var style = (ctx.props || {}).style;
			var text = localised(when, style);
			if (text) element.textContent = text;
		},
	});

	/* localised writes one instant the way the reader's locale writes it, and
	 * returns nothing when it cannot -- which leaves the server's sentence. */
	function localised(when, style) {
		try {
			if (style === 'relative') {
				if (typeof Intl === 'undefined' || !Intl.RelativeTimeFormat) return '';
				return elapsed(when);
			}
			if (typeof Intl === 'undefined' || !Intl.DateTimeFormat) return '';
			if (style === 'time') return when.toLocaleTimeString();
			if (style === 'datetime') return when.toLocaleString();
			if (style === 'date') return when.toLocaleDateString();
			return '';
		} catch (error) {
			return '';
		}
	}

	/* elapsed says how long ago, in the largest unit that still has a whole
	 * number in it: "3 hours ago" and not "180 minutes ago".
	 *
	 * The table is walked from the largest down, so the first unit whose span
	 * fits is the one used. Months are 30 days and years are 365, which is
	 * wrong by up to a day and a half and is invisible at the scale where
	 * those units are chosen -- "2 months ago" does not become wrong because
	 * February is short.
	 */
	function elapsed(when) {
		var seconds = Math.round((when.getTime() - Date.now()) / 1000);
		var units = [
			['year', 31536000],
			['month', 2592000],
			['week', 604800],
			['day', 86400],
			['hour', 3600],
			['minute', 60],
		];
		var format = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });
		for (var at = 0; at < units.length; at++) {
			var span = units[at][1];
			if (Math.abs(seconds) >= span) {
				return format.format(Math.round(seconds / span), units[at][0]);
			}
		}
		return format.format(seconds, 'second');
	}

	/* ---- sortable ------------------------------------------------------------
	 *
	 * Reorders a list in place, by drag and by keyboard, so a form that posts
	 * the list's inputs posts them in the order the person chose -- the order
	 * is the DOM's, and nothing else holds a copy of it.
	 *
	 * The markup: data-kyse-behavior="sortable" on the list, data-sortable-item
	 * on each entry. An entry may carry a button with data-sortable-up and one
	 * with data-sortable-down, which move it one place; Alt+ArrowUp and
	 * Alt+ArrowDown do the same from anywhere inside the entry and keep the
	 * focus where it was. Dragging an entry by itself, or by its
	 * data-sortable-handle when it has one, drops it before or after the entry
	 * under the pointer. The first entry's "up" and the last one's "down" are
	 * disabled, so a keyboard never presses a button that does nothing.
	 *
	 * After every move a live region inside the list, data-part="feedback",
	 * says where the entry went, and the list dispatches "kyse:sorted"
	 * (bubbling) for a page that wants to know. The sentence is the list's
	 * data-sortable-moved, with {label}, {position} and {count} filled in --
	 * the application's own language -- or an English one.
	 *
	 * Without this script the buttons do nothing and the order stays the one
	 * the server drew, which is a list that still submits.
	 */
	arandu.ui.define('sortable', {
		mounted: function (ctx) {
			var list = ctx.element;
			var entries = function () {
				var out = [];
				for (var i = 0; i < list.children.length; i++) {
					if (list.children[i].hasAttribute('data-sortable-item')) out.push(list.children[i]);
				}
				return out;
			};
			var refresh = function () {
				var all = entries();
				for (var i = 0; i < all.length; i++) {
					var up = all[i].querySelector('[data-sortable-up]');
					var down = all[i].querySelector('[data-sortable-down]');
					if (up) up.disabled = i === 0;
					if (down) down.disabled = i === all.length - 1;
					if (!all[i].querySelector('[data-sortable-handle]')) all[i].setAttribute('draggable', 'true');
				}
				each(list, '[data-sortable-handle]', function (handle) {
					handle.setAttribute('draggable', 'true');
				});
			};
			var say = function (entry) {
				var all = entries();
				var region = list.querySelector('[data-part="feedback"]');
				if (region) {
					var label = entry.getAttribute('data-sortable-label') || entry.textContent.replace(/\s+/g, ' ').trim();
					var sentence = list.getAttribute('data-sortable-moved') || '{label} moved to position {position} of {count}';
					region.textContent = sentence.replace('{label}', label)
						.replace('{position}', String(all.indexOf(entry) + 1))
						.replace('{count}', String(all.length));
				}
				list.dispatchEvent(new CustomEvent('kyse:sorted', { bubbles: true, detail: { item: entry } }));
			};
			var move = function (entry, step) {
				var all = entries();
				var at = all.indexOf(entry);
				var to = at + step;
				if (at < 0 || to < 0 || to >= all.length) return false;
				if (step < 0) list.insertBefore(entry, all[to]);
				else list.insertBefore(entry, all[to].nextSibling);
				refresh();
				say(entry);
				return true;
			};
			ctx.click = function (event) {
				var button = event.target.closest('[data-sortable-up], [data-sortable-down]');
				if (!button || !list.contains(button)) return;
				var entry = button.closest('[data-sortable-item]');
				if (!entry) return;
				event.preventDefault();
				move(entry, button.hasAttribute('data-sortable-up') ? -1 : 1);
				/* A disabled button cannot hold the focus, so a press that
				 * moved an entry to an end hands it to the other button. */
				if (button.disabled) {
					var other = entry.querySelector(button.hasAttribute('data-sortable-up') ? '[data-sortable-down]' : '[data-sortable-up]');
					if (other) other.focus();
				} else {
					button.focus();
				}
			};
			ctx.keydown = function (event) {
				if (!event.altKey || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return;
				var entry = event.target.closest('[data-sortable-item]');
				if (!entry || entry.parentNode !== list) return;
				var focused = document.activeElement;
				if (move(entry, event.key === 'ArrowUp' ? -1 : 1)) {
					event.preventDefault();
					if (focused && typeof focused.focus === 'function') focused.focus();
				}
			};
			ctx.dragstart = function (event) {
				var entry = event.target.closest('[data-sortable-item]');
				if (!entry || entry.parentNode !== list) return;
				ctx.dragging = entry;
				entry.setAttribute('data-dragging', 'true');
				if (event.dataTransfer) {
					event.dataTransfer.effectAllowed = 'move';
					/* Firefox starts no drag without data. */
					event.dataTransfer.setData('text/plain', '');
				}
			};
			ctx.dragover = function (event) {
				if (!ctx.dragging) return;
				var over = event.target.closest('[data-sortable-item]');
				event.preventDefault();
				if (!over || over === ctx.dragging || over.parentNode !== list) return;
				var box = over.getBoundingClientRect();
				var after = event.clientY > box.top + box.height / 2;
				list.insertBefore(ctx.dragging, after ? over.nextSibling : over);
			};
			ctx.drop = function (event) {
				if (ctx.dragging) event.preventDefault();
			};
			ctx.dragend = function () {
				if (!ctx.dragging) return;
				var entry = ctx.dragging;
				ctx.dragging = null;
				entry.removeAttribute('data-dragging');
				refresh();
				say(entry);
			};
			refresh();
			list.addEventListener('click', ctx.click);
			list.addEventListener('keydown', ctx.keydown);
			list.addEventListener('dragstart', ctx.dragstart);
			list.addEventListener('dragover', ctx.dragover);
			list.addEventListener('drop', ctx.drop);
			list.addEventListener('dragend', ctx.dragend);
		},
		destroyed: function (ctx) {
			var list = ctx.element;
			if (!ctx.click) return;
			list.removeEventListener('click', ctx.click);
			list.removeEventListener('keydown', ctx.keydown);
			list.removeEventListener('dragstart', ctx.dragstart);
			list.removeEventListener('dragover', ctx.dragover);
			list.removeEventListener('drop', ctx.drop);
			list.removeEventListener('dragend', ctx.dragend);
		},
	});

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
