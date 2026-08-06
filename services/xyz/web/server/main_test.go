package main
import("net/http/httptest";"testing")
func TestHealth(t *testing.T){r:=httptest.NewRecorder(); newHandler("").ServeHTTP(r,httptest.NewRequest("GET","/healthz",nil)); if r.Code!=200{t.Fatalf("status %d",r.Code)}}
