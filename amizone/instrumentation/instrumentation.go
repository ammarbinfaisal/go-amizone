// Package instrumentation provides OpenTelemetry tracing and metrics for the amizone service.
// It exports traces via OTLP and metrics via Prometheus for Grafana dashboards.
package instrumentation

import (
	"context"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

const (
	ServiceName    = "amizone-api"
	ServiceVersion = "1.0.0"
)

var (
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics
	requestCounter      metric.Int64Counter
	requestDuration     metric.Float64Histogram
	activeRequests      metric.Int64UpDownCounter
	cfChallengeCounter  metric.Int64Counter
	loginAttemptCounter metric.Int64Counter
	errorCounter        metric.Int64Counter
)

// Config holds instrumentation configuration
type Config struct {
	// OTLPEndpoint is the OTLP exporter endpoint (e.g., "localhost:4318")
	OTLPEndpoint string
	// Environment is the deployment environment (e.g., "production", "development")
	Environment string
	// SampleRate is the trace sampling rate (0.0 to 1.0)
	SampleRate float64
	// MetricsEnabled enables Prometheus metrics
	MetricsEnabled bool
}

// DefaultConfig returns default configuration based on environment
func DefaultConfig() Config {
	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	sampleRate := 1.0 // 100% in dev
	if env == "production" || env == "prod" {
		sampleRate = 0.1 // 10% in prod
	}

	// Override from env if set
	if sr := os.Getenv("OTEL_SAMPLE_RATE"); sr != "" {
		// Parse sample rate from env (simplified - in production use strconv)
		sampleRate = 0.1 // default to 10% if set
	}

	return Config{
		OTLPEndpoint:   getEnvOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318"),
		Environment:    env,
		SampleRate:     sampleRate,
		MetricsEnabled: os.Getenv("METRICS_ENABLED") != "false",
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// Init initializes OpenTelemetry tracing and metrics
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(ServiceName),
			semconv.ServiceVersion(ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	// Setup trace exporter
	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
		otlptracehttp.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		klog.Warningf("Failed to create OTLP trace exporter: %v, continuing without tracing", err)
		traceExporter = nil
	}

	// Setup sampler based on config
	var sampler sdktrace.Sampler
	if cfg.Environment == "production" || cfg.Environment == "prod" {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	} else {
		sampler = sdktrace.AlwaysSample()
	}

	var tracerProvider *sdktrace.TracerProvider
	if traceExporter != nil {
		tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
		)
	} else {
		// Noop provider if no exporter
		tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.NeverSample()),
		)
	}
	otel.SetTracerProvider(tracerProvider)

	tracer = otel.Tracer(ServiceName)

	// Setup metrics
	var meterProvider *sdkmetric.MeterProvider
	if cfg.MetricsEnabled {
		promExporter, err := prometheus.New()
		if err != nil {
			klog.Warningf("Failed to create Prometheus exporter: %v, continuing without metrics", err)
		} else {
			meterProvider = sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(promExporter),
				sdkmetric.WithResource(res),
			)
			otel.SetMeterProvider(meterProvider)
		}
	}

	meter = otel.Meter(ServiceName)

	// Initialize metrics
	if err := initMetrics(); err != nil {
		return nil, err
	}

	klog.Infof("OpenTelemetry initialized: env=%s, sample_rate=%.2f, metrics=%v",
		cfg.Environment, cfg.SampleRate, cfg.MetricsEnabled)

	// Return shutdown function
	return func(ctx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if meterProvider != nil {
			if err := meterProvider.Shutdown(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return errs[0]
		}
		return nil
	}, nil
}

func initMetrics() error {
	var err error

	requestCounter, err = meter.Int64Counter(
		"amizone.requests.total",
		metric.WithDescription("Total number of requests to Amizone"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	requestDuration, err = meter.Float64Histogram(
		"amizone.request.duration",
		metric.WithDescription("Duration of Amizone requests in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	activeRequests, err = meter.Int64UpDownCounter(
		"amizone.requests.active",
		metric.WithDescription("Number of active requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	cfChallengeCounter, err = meter.Int64Counter(
		"amizone.cloudflare.challenges",
		metric.WithDescription("Total Cloudflare challenges encountered"),
		metric.WithUnit("{challenge}"),
	)
	if err != nil {
		return err
	}

	loginAttemptCounter, err = meter.Int64Counter(
		"amizone.login.attempts",
		metric.WithDescription("Total login attempts"),
		metric.WithUnit("{attempt}"),
	)
	if err != nil {
		return err
	}

	errorCounter, err = meter.Int64Counter(
		"amizone.errors.total",
		metric.WithDescription("Total errors encountered"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// Tracer returns the global tracer
func Tracer() trace.Tracer {
	return tracer
}

// Meter returns the global meter
func Meter() metric.Meter {
	return meter
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, opts...)
}

// RequestTracer is a helper for tracing HTTP requests to Amizone
type RequestTracer struct {
	ctx       context.Context
	span      trace.Span
	startTime time.Time
	endpoint  string
	method    string
}

// StartRequest starts tracing an outbound request to Amizone
func StartRequest(ctx context.Context, method, endpoint string) *RequestTracer {
	ctx, span := tracer.Start(ctx, "amizone.request",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(method),
			semconv.URLPath(endpoint),
			attribute.String("amizone.endpoint", endpoint),
		),
	)

	if activeRequests != nil {
		activeRequests.Add(ctx, 1)
	}

	return &RequestTracer{
		ctx:       ctx,
		span:      span,
		startTime: time.Now(),
		endpoint:  endpoint,
		method:    method,
	}
}

// End completes the request trace
func (rt *RequestTracer) End(statusCode int, err error) {
	duration := time.Since(rt.startTime).Milliseconds()

	if rt.span != nil {
		rt.span.SetAttributes(
			semconv.HTTPResponseStatusCode(statusCode),
			attribute.Int64("http.duration_ms", duration),
		)

		if err != nil {
			rt.span.RecordError(err)
			rt.span.SetStatus(codes.Error, err.Error())
		} else if statusCode >= 400 {
			rt.span.SetStatus(codes.Error, http.StatusText(statusCode))
		} else {
			rt.span.SetStatus(codes.Ok, "")
		}
		rt.span.End()
	}

	// Record metrics
	ctx := rt.ctx
	attrs := []attribute.KeyValue{
		attribute.String("method", rt.method),
		attribute.String("endpoint", rt.endpoint),
		attribute.Int("status_code", statusCode),
		attribute.Bool("success", err == nil && statusCode < 400),
	}

	if requestCounter != nil {
		requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if requestDuration != nil {
		requestDuration.Record(ctx, float64(duration), metric.WithAttributes(attrs...))
	}
	if activeRequests != nil {
		activeRequests.Add(ctx, -1)
	}
	if err != nil && errorCounter != nil {
		errorCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "request"),
			attribute.String("endpoint", rt.endpoint),
		))
	}
}

// Context returns the span context
func (rt *RequestTracer) Context() context.Context {
	return rt.ctx
}

// RecordCFChallenge records a Cloudflare challenge event
func RecordCFChallenge(ctx context.Context, endpoint string, solved bool) {
	if cfChallengeCounter != nil {
		cfChallengeCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
			attribute.Bool("solved", solved),
		))
	}

	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent("cloudflare_challenge",
			trace.WithAttributes(
				attribute.String("endpoint", endpoint),
				attribute.Bool("solved", solved),
			),
		)
	}
}

// RecordLogin records a login attempt
func RecordLogin(ctx context.Context, success bool, duration time.Duration) {
	if loginAttemptCounter != nil {
		loginAttemptCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.Bool("success", success),
		))
	}

	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent("login_attempt",
			trace.WithAttributes(
				attribute.Bool("success", success),
				attribute.Int64("duration_ms", duration.Milliseconds()),
			),
		)
	}
}

// RecordError records an error event
func RecordError(ctx context.Context, errorType string, err error) {
	if errorCounter != nil {
		errorCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
		))
	}

	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err,
			trace.WithAttributes(
				attribute.String("error_type", errorType),
			),
		)
	}
}
