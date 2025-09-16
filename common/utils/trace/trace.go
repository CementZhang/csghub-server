package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

const (
	HeaderRequestID   = "X-Request-ID"
	HeaderTraceparent = "traceparent"
	HeaderXB3         = "X-B3-TraceId"
	HeaderKong        = "X-Kong-Request-Id"
)

// traceContextKey is used as the key for the trace ID in context.Context.
// Using a private custom type avoids key collisions.
type traceContextKey struct{}

var (
	// Standard and common trace ID headers
	traceHeaders = []string{
		HeaderTraceparent,
		HeaderRequestID,
		HeaderXB3,
		HeaderKong,
	}
)

// GetOrGenTraceID retrieves the trace ID from the Gin context.
// It checks for standard trace headers, then the OpenTelemetry span,
// and finally generates a new ID if none is found.
// IMPORTANT: It now also injects the trace ID into the request's context.Context.
func GetOrGenTraceID(c *gin.Context) string {
	traceID := GetTraceIDInGinContext(c)
	if traceID == "" {
		traceID = uuid.New().String()
	}
	// If no trace ID is found in headers, generate a new one
	c.Set(HeaderRequestID, traceID)
	reqCtx := setTraceIDInRequestContext(c.Request.Context(), traceID)
	c.Request = c.Request.WithContext(reqCtx)
	return traceID
}

func GetTraceIDInGinContext(c *gin.Context) string {
	if nil == c {
		return ""
	}
	// 1. Check for gin context first; this simulates cache
	if traceID, ok := c.Get(HeaderRequestID); ok {
		if tid, ok := traceID.(string); ok {
			return tid
		}
	}

	// 2. Try to get trace ID from OpenTelemetry span in the context/; connection with monitor and log
	if nil == c.Request || nil == c.Request.Context() {
		return ""
	}
	span := trace.SpanFromContext(c.Request.Context())
	if span.SpanContext().HasTraceID() {
		traceID := span.SpanContext().TraceID().String()
		return traceID
	}

	// 3. Check for standard trace headers
	for _, header := range traceHeaders {
		headerValue := c.Request.Header.Get(header)
		if headerValue == "" {
			continue
		}
		if header == HeaderTraceparent {
			// W3C Trace Context format: version-traceid-spanid-traceflags
			parts := strings.Split(headerValue, "-")
			if len(parts) == 4 {
				return parts[1]
			}
			continue
		}
		return headerValue
	}
	return ""
}

// setTraceIDInRequestContext injects the trace ID into the request's context.Context.
func setTraceIDInRequestContext(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceContextKey{}, traceID)
}

// GetOrGenTraceIDFromContext tries to get trace info from otel span, or generates new ones.
// It returns the traceID, the full traceparent header string, and whether it's newly generated.
func GetOrGenTraceIDFromContext(ctx context.Context) (traceID, traceParent string, isNew bool) {
	if nil == ctx {
		return "", "", false
	}
	// get trace id from otel span
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() && spanCtx.TraceID().String() != (trace.TraceID{}).String() {
		traceID = spanCtx.TraceID().String()
		spanID := spanCtx.SpanID().String()
		// Ensure spanID is valid before creating traceParent
		if spanID != (trace.SpanID{}).String() {
			traceParent = fmt.Sprintf("00-%s-%s-%02x", traceID, spanID, spanCtx.TraceFlags())
			return traceID, traceParent, false
		}
	}

	// generate a new trace id and span id
	traceID = strings.ReplaceAll(uuid.New().String(), "-", "")
	spanIDBytes := make([]byte, 8)
	// crypto/rand.Read is a good source of entropy for span IDs
	_, _ = rand.Read(spanIDBytes)
	spanID := hex.EncodeToString(spanIDBytes)
	// version 00, sampled flag 01
	traceParent = fmt.Sprintf("00-%s-%s-01", traceID, spanID)

	return traceID, traceParent, true
}
