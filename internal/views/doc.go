// Package views is the laptop side of the phone's views (mobile plan
// 3.11, MOBUX-15): the view definition (Def: source, params, typed
// fields, layouts, default spec), the full check of a view spec against
// its Def, and the renderer that turns the rows a source gave into one
// block (filter, sort, group, limit, layout, density).
//
// The phone only draws the block. A spec is data only: it names fields,
// params and layouts the Def allows and never carries SQL, a shell line,
// an absolute path or a URL. Data is always fetched on the laptop by the
// view's Fetch (a topic snapshot, an op handler, a read-only tool) and
// the spec is applied here, in memory, on what the source returned.
//
// The spec type itself lives in internal/blocks (a block carries the spec
// that made it); this package checks it against a Def and renders it.
package views
