// Copyright (C) 2016 AppNeta, Inc. All rights reserved.

package tv

import "github.com/appneta/go-appneta/v1/tv/internal/traceview"

// Trace represents a distributed trace for this request that reports
// events to AppNeta TraceView.
type Trace interface {
	// Inherited from the Layer interface
	//  BeginLayer(layerName string, args ...interface{}) Layer
	//  BeginProfile(profileName string, args ...interface{}) Profile
	//	End(args ...interface{})
	//	Info(args ...interface{})
	//  Error(class, msg string)
	//  Err(error)
	//  IsTracing() bool
	Layer

	// End a Trace, and include KV pairs returned by func f. Useful alternative to End() when used
	// with defer to delay evaluation of KVs until the end of the trace (since a deferred function's
	// arguments are evaluated when the defer statement is evaluated). Func f will not be called at
	// all if this span is not tracing.
	EndCallback(f func() KVMap)

	// ExitMetadata returns a hex string that propagates the end of this span back to a remote
	// client. It is typically used in an response header (e.g. the HTTP Header "X-Trace"). Call
	// this method to set a response header in advance of calling End().
	ExitMetadata() string
}

// KVMap is a map of additional key-value pairs to report along with the event data provided
// to TraceView. Certain key names (such as "Query" or "RemoteHost") are used by AppNeta to
// provide details about program activity and distinguish between different types of layers.
// Please visit http://docs.appneta.com/traceview-instrumentation#special-interpretation for
// details on the key names that TraceView looks for.
type KVMap map[string]interface{}

type tvTrace struct {
	layerSpan
	exitEvent traceview.Event
}

func (t *tvTrace) tvContext() traceview.Context { return t.tvCtx }

// NewTrace creates a new Trace for reporting to TraceView and immediately records
// the beginning of the layer layerName. If this trace is sampled, it may report
// event data to AppNeta; otherwise event reporting will be a no-op.
func NewTrace(layerName string) Trace {
	ctx, ok := traceview.NewContext(layerName, "", true, nil)
	if !ok {
		return &nullTrace{}
	}
	return &tvTrace{
		layerSpan: layerSpan{span: span{tvCtx: ctx, labeler: layerLabeler{layerName}}},
	}
}

// NewTraceFromID creates a new Trace for reporting to TraceView, provided an
// incoming trace ID (e.g. from a incoming RPC or service call's "X-Trace" header).
// If callback is provided & trace is sampled, cb will be called for entry event KVs
func NewTraceFromID(layerName, mdstr string, cb func() KVMap) Trace {
	ctx, ok := traceview.NewContext(layerName, mdstr, true, func() map[string]interface{} {
		if cb != nil {
			return cb()
		}
		return nil
	})
	if !ok {
		return &nullTrace{}
	}
	return &tvTrace{
		layerSpan: layerSpan{span: span{tvCtx: ctx, labeler: layerLabeler{layerName}}},
	}
}

// EndTrace reports the exit event for the layer name that was used when calling NewTrace().
// No more events should be reported from this trace.
func (t *tvTrace) End(args ...interface{}) {
	if t.ok() {
		t.AddEndArgs(args...)
		t.reportExit()
	}
}

// EndCallback ends a Trace, reporting additional KV pairs returned by calling cb
func (t *tvTrace) EndCallback(cb func() KVMap) {
	if t.ok() {
		if cb != nil {
			var args []interface{}
			for k, v := range cb() {
				args = append(args, k, v)
			}
			t.AddEndArgs(args...)
		}
		t.reportExit()
	}
}

func (t *tvTrace) reportExit() {
	if t.ok() {
		t.lock.Lock()
		defer t.lock.Unlock()
		for _, edge := range t.childEdges { // add Edge KV for each joined child
			t.endArgs = append(t.endArgs, "Edge", edge)
		}
		if t.exitEvent != nil { // use exit event, if one was provided
			_ = t.exitEvent.ReportContext(t.tvCtx, true, t.endArgs...)
		} else {
			_ = t.tvCtx.ReportEvent(traceview.LabelExit, t.layerName(), t.endArgs...)
		}
		t.childEdges = nil // clear child edge list
		t.endArgs = nil
		t.ended = true
	}
}

func (t *tvTrace) IsTracing() bool { return t.tvCtx.IsTracing() }

// ExitMetadata reports the X-Trace metadata string that will be used by the exit event.
// This is useful for setting response headers before reporting the end of the span.
func (t *tvTrace) ExitMetadata() (mdHex string) {
	if t.IsTracing() {
		if t.exitEvent == nil {
			t.exitEvent = t.tvCtx.NewEvent(traceview.LabelExit, t.layerName(), false)
		}
		if t.exitEvent != nil {
			mdHex = t.exitEvent.MetadataString()
		}
	}
	return
}

// A nullTrace is not tracing.
type nullTrace struct{ nullSpan }

func (t *nullTrace) EndCallback(f func() KVMap) {}
func (t *nullTrace) ExitMetadata() string       { return "" }
