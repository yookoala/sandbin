# Decision 0025: the harness picker opens as a modal on small screens

Decided at plan open, HEAD `348b102`, from the user's follow-up on the shipped 0024 pages.

## Why

The anchored dropdown (`.pick-pop`, `position:absolute` under the select-style face,
`min-width:100%`) is built for desktop width. On a phone it hangs off the inline "I use ____."
sentence toward the right edge and its 14em list is a cramped half-screen. The request: on
small screens the selection becomes a modal dialog filling most of the screen — input box on
top, scrollable list below it.

## Decisions

1. **Trigger is a pure CSS media query at viewport width ≤ 30rem (480px).** The picker DOM and
   the entire decision-0023 item-4 behavior contract (pinned pair above the separator, matches
   float, blur commits, Enter/arrows/Escape, copy flash, swap fade) stay identical at every
   width; only the popup's box model changes. No JS resize listeners, no matchMedia branching.
2. **Modal box model.** In the query, `.pick-pop` becomes `position:fixed` with
   `left:0.75rem; right:0.75rem; top:6vh; bottom:6vh` (≈88% of the viewport height — "most of
   the screen"), and takes the panel look the desktop list already has: white background, the
   same border/radius, a shadow, inner padding. Its type box goes full width; the `ul` sheds
   its own border/shadow/margins and gains `flex:1; max-height:none` so it fills the remaining
   height and scrolls (the flex-column popup already stacks input above list).
3. **A scrim behind the modal.** front.html gains `<span class="pick-scrim" id="pick-scrim"
   hidden>` inside the `.pick` span (span, to match the picker's inline markup); `sandbinOpenPick`/`sandbinClosePick` toggle it alongside `#pick-pop`. It is
   `display:none` outside the media query, `position:fixed; inset:0` with a translucent dark
   wash behind the panel (z-ordered under it) inside. Clicking the scrim blurs the focused type
   box, and the existing blur handler then commits the highlighted row and closes — the same
   outcome as clicking anywhere else today, so no new JS path. Escape/arrows/Enter unchanged.
4. **Desktop mode is byte-identical behavior.** The media query adds rules only below 30rem;
   the anchored dropdown, its 14em max-height, and its z-index stay as they are.
5. **The swap animation gains a warm fade.** The `sandbin-swap` move/opacity stays 0.28s; the
   swapping widgets (`#cmd-input`, `#cmd-path`, `#pick-face`) additionally run a
   `sandbin-swapbg` keyframe — background `#f7e7cb` fading to white over 1.4s — so a changed
   widget is briefly tinted before settling (mockup review round 2).
6. **The path line reads "Skill will be installed to:"** — the "Skill Install Path:" wording is
   retired (mockup review round 2); the path box's `aria-label` stays "Skill install path".
7. **Inside the mobile modal the sentence survives.** A `<span class="pick-hint">I use</span>`
   (aria-hidden) sits above the type box, `display:none` on desktop and shown in the query, and
   the type box carries `placeholder="input harness name"` styled muted (`#8b8171`, weight 400)
   so it reads as a hint, not a value (mockup review round 2).
8. **A closing instruction paragraph.** After the path box: "Then tell your agent to use the
   skill `<code>sandbin-preview</code>` to show you HTML / ANSI preview here." — the user's
   copy, verbatim, with `sandbin-preview` as a code chip (mockup review round 2).
9. **The landing tagline is retitled.** It reads "A small preview service for AI agents to show
   you HTML+JS+CSS interactive preview / presentation or ANSI design mock up." — the user's
   copy with the trailing "with" dropped by their choice (mockup review round 2); the "Learn
   more about Sandbin on GitHub." line below it stays.

## Skipped on purpose

- `<dialog>`/`showModal()`, focus trapping, inert/aria-modal semantics, and scroll-locking of
  the page behind the modal — the combobox keeps its 0023 ARIA shape.
- Height-triggered modal (short landscape phones): the 14em scrollable list already fits them.
- Docs pages (no picker), bin routes, footer/nav work from 0024.
