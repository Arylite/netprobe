// Package alert watches the results and tells the channels when something
// goes wrong, and when it stops.
//
// An incident opens when a check fails a given number of times in a row on an
// edge, or when an edge stops reporting, and resolves when the check succeeds
// again or the edge reports again. Each channel is told of each event until it
// answers, and no longer than a bound: an alert that comes an hour late is not
// an alert.
package alert
