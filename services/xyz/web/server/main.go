package main
import(
 "context"
 "crypto/rand"
 "embed"
 "encoding/hex"
 "encoding/json"
 "io"
 "io/fs"
 "log/slog"
 "net/http"
 "net/http/httputil"
 "net/url"
 "os"
 "os/signal"
 "strconv"
 "strings"
 "sync/atomic"
 "syscall"
 "time"
 "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
 "go.opentelemetry.io/otel"
 "go.opentelemetry.io/otel/attribute"
 "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
 "go.opentelemetry.io/otel/propagation"
 "go.opentelemetry.io/otel/sdk/resource"
 sdktrace "go.opentelemetry.io/otel/sdk/trace"
 "go.opentelemetry.io/otel/trace"
)
//go:embed static/*
var assets embed.FS
var requests atomic.Uint64
type statusWriter struct{http.ResponseWriter;status int}
func(w *statusWriter)WriteHeader(status int){w.status=status;w.ResponseWriter.WriteHeader(status)}
func main(){
 slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout,nil)))
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
 shutdown,err:=configureTracing(ctx);if err!=nil{slog.Error("configure tracing","error",err);os.Exit(1)};defer func(){flush,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();_ = shutdown(flush)}()
 server:=&http.Server{Addr:":8080",Handler:otelhttp.NewHandler(newHandler("http://xyz-api:8080"),"http.server"),ReadHeaderTimeout:5*time.Second,ReadTimeout:15*time.Second,WriteTimeout:15*time.Second,IdleTimeout:30*time.Second}
 errors:=make(chan error,1);go func(){errors<-server.ListenAndServe()}();slog.Info("web listening","application","xyz")
 select{case err:=<-errors:if err!=nil&&err!=http.ErrServerClosed{slog.Error("server stopped","error",err);os.Exit(1)};case <-ctx.Done():shutdownCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel();if err:=server.Shutdown(shutdownCtx);err!=nil{slog.Error("graceful shutdown","error",err);os.Exit(1)}}
}
func newHandler(proxyTarget string)http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("/healthz",func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(http.StatusOK)})
 mux.HandleFunc("/readyz",func(w http.ResponseWriter,_ *http.Request){w.WriteHeader(http.StatusOK)})
 mux.HandleFunc("/metrics",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Content-Type","text/plain; version=0.0.4");_,_=io.WriteString(w,"http_requests_total{application=\\\"xyz\\\",namespace=\\\""+value("STEADYSTATE_APP_NAMESPACE","local")+"\\\",version=\\\""+value("STEADYSTATE_APP_VERSION","development")+"\\\",status=\\\"200\\\"} "+strconv.FormatUint(requests.Load(),10)+"\\n")})
 mux.HandleFunc("/.well-known/runtime",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Content-Type","application/json");_=json.NewEncoder(w).Encode(map[string]string{"application":"xyz","namespace":value("STEADYSTATE_APP_NAMESPACE","local"),"version":value("STEADYSTATE_APP_VERSION","development")})})
 if proxyTarget!=""{target,_:=url.Parse(proxyTarget);proxy:=httputil.NewSingleHostReverseProxy(target);mux.Handle("/api/",http.StripPrefix("/api",proxy))}
 built,_:=fs.Sub(assets,"static");files:=http.FileServer(http.FS(built));mux.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/"{if _,err:=fs.Stat(built,strings.TrimPrefix(r.URL.Path,"/"));err!=nil{r.URL.Path="/"}};if strings.Contains(r.URL.Path,"assets/"){w.Header().Set("Cache-Control","public,max-age=31536000,immutable")}else{w.Header().Set("Cache-Control","no-cache")};files.ServeHTTP(w,r)})
 return requestMiddleware(mux)
}
func requestMiddleware(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){id:=r.Header.Get("X-Request-ID");if id==""{id=secureID()};w.Header().Set("X-Request-ID",id);record:=&statusWriter{ResponseWriter:w,status:http.StatusOK};start:=time.Now();next.ServeHTTP(record,r);if r.URL.Path!="/healthz"&&r.URL.Path!="/readyz"&&r.URL.Path!="/metrics"{requests.Add(1);span:=trace.SpanFromContext(r.Context()).SpanContext();slog.Info("http request","request_id",id,"trace_id",span.TraceID().String(),"span_id",span.SpanID().String(),"method",r.Method,"route",normalized(r.URL.Path),"status",record.status,"latency_ms",time.Since(start).Milliseconds(),"application","xyz","namespace",value("STEADYSTATE_APP_NAMESPACE","local"),"version",value("STEADYSTATE_APP_VERSION","development"))}})}
func secureID()string{value:=make([]byte,16);if _,err:=rand.Read(value);err!=nil{return strconv.FormatInt(time.Now().UnixNano(),16)};return hex.EncodeToString(value)}
func normalized(path string)string{if strings.HasPrefix(path,"/api/"){return "/api/*"};return path}
func value(key,fallback string)string{if v:=os.Getenv(key);v!=""{return v};return fallback}
func configureTracing(ctx context.Context)(func(context.Context)error,error){
 otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{},propagation.Baggage{}));endpoint:=os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT");if endpoint==""{return func(context.Context)error{return nil},nil}
 exporter,err:=otlptracegrpc.New(ctx,otlptracegrpc.WithEndpoint(endpoint),otlptracegrpc.WithInsecure());if err!=nil{return nil,err}
 platformResource,err:=resource.New(ctx,resource.WithAttributes(attribute.String("service.name","xyz"),attribute.String("service.namespace",value("STEADYSTATE_APP_NAMESPACE","local")),attribute.String("service.version",value("STEADYSTATE_APP_VERSION","development")),attribute.String("steadystate.application","xyz")));if err!=nil{return nil,err}
 provider:=sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter),sdktrace.WithResource(platformResource));otel.SetTracerProvider(provider);return provider.Shutdown,nil
}
