package main

import "time"

const (
	blockStep = 30 * time.Minute // each failure adds this much
	blockMax  = 24 * time.Hour   // a single block never gets longer than this

	// shortcut: an address idle for 30 days forgets its failure count; raise the window if
	// long-lived deployments need escalation to survive that long.
	blockIdlePrune = 30 * 24 * time.Hour
)
