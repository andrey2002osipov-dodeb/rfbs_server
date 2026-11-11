package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServerMode(t *testing.T) (*serverMode, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OZON_RFBS_ADDR", "127.0.0.1:0")
	t.Setenv("OZON_RFBS_ADMIN_USER", "admin")
	t.Setenv("OZON_RFBS_ADMIN_PASSWORD", "password1")
	m, err := envServerMode(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv = m
	t.Cleanup(func() { srv = nil })
	store := LoadStore(dir)
	mgr := NewManager(dir, store, NewMockRegistry())
	s := NewServer(dir, store, mgr)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("page")) })
	s.Routes(mux)
	m.routes(mux, s)
	return m, m.wrap(mux)
}

func do(h http.Handler, method, path, body, cookie string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "omv:8766"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func login(t *testing.T, h http.Handler, user, pass string) string {
	t.Helper()
	w := do(h, "POST", "/api/login", `{"user":"`+user+`","password":"`+pass+`"}`, "", nil)
	if w.Code != 200 {
		t.Fatalf("вход %s: %d %s", user, w.Code, w.Body)
	}
	return strings.Split(w.Header().Get("Set-Cookie"), ";")[0]
}

func TestServerAuth(t *testing.T) {
	m, h := newTestServerMode(t)

	// без входа: страницы → /login, API → 401
	if w := do(h, "GET", "/", "", "", nil); w.Code != 302 || w.Header().Get("Location") != "/login" {
		t.Fatalf("страница без входа: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", "", nil); w.Code != 401 {
		t.Fatalf("API без входа: %d", w.Code)
	}
	if w := do(h, "GET", "/api/ping", "", "", nil); w.Code != 200 {
		t.Fatalf("ping: %d", w.Code)
	}
	// неверный пароль
	if w := do(h, "POST", "/api/login", `{"user":"admin","password":"nope"}`, "", nil); w.Code != 401 {
		t.Fatalf("неверный пароль: %d", w.Code)
	}
	c := login(t, h, "ADMIN", "password1") // логин без учёта регистра
	if w := do(h, "GET", "/api/state", "", c, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"server":true`) {
		t.Fatalf("state после входа: %d %s", w.Code, w.Body)
	}
	// чужой сайт
	if w := do(h, "GET", "/api/state", "", c, map[string]string{"Origin": "http://evil.example"}); w.Code != 403 {
		t.Fatalf("чужой Origin: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", c, map[string]string{"Origin": "http://omv:8766"}); w.Code != 200 {
		t.Fatalf("свой Origin: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", c, map[string]string{"Origin": "https://kucha.ts.net", "X-Forwarded-Host": "kucha.ts.net"}); w.Code != 200 {
		t.Fatalf("Origin за прокси: %d", w.Code)
	}
	// подделанная cookie
	if w := do(h, "GET", "/api/state", "", c+"x", nil); w.Code != 401 {
		t.Fatalf("подделанная cookie: %d", w.Code)
	}
	// на сервере нельзя закрыть программу
	if w := do(h, "POST", "/api/quit", "{}", c, nil); w.Code != 400 {
		t.Fatalf("quit: %d", w.Code)
	}
	// пользователи
	if w := do(h, "POST", "/api/users/add", `{"name":"ivan","password":"short"}`, c, nil); w.Code != 400 {
		t.Fatalf("короткий пароль: %d", w.Code)
	}
	if w := do(h, "POST", "/api/users/add", `{"name":"ivan","password":"ivanpass1"}`, c, nil); w.Code != 200 {
		t.Fatalf("добавить: %d %s", w.Code, w.Body)
	}
	ci := login(t, h, "ivan", "ivanpass1")
	if w := do(h, "POST", "/api/users/delete", `{"name":"ivan"}`, ci, nil); w.Code != 400 {
		t.Fatalf("удалить себя: %d", w.Code)
	}
	// смена пароля завершает старые сессии, а сменившему выдаётся новая cookie
	w := do(h, "POST", "/api/users/password", `{"name":"admin","password":"password2"}`, c, nil)
	if w.Code != 200 {
		t.Fatalf("смена пароля: %d %s", w.Code, w.Body)
	}
	nc := strings.Split(w.Header().Get("Set-Cookie"), ";")[0]
	if w := do(h, "GET", "/api/state", "", c, nil); w.Code != 401 {
		t.Fatalf("старая сессия после смены пароля: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", nc, nil); w.Code != 200 {
		t.Fatalf("новая cookie: %d", w.Code)
	}
	if w := do(h, "POST", "/api/users/delete", `{"name":"ivan"}`, nc, nil); w.Code != 200 {
		t.Fatalf("удалить ivan: %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "", ci, nil); w.Code != 401 {
		t.Fatalf("сессия удалённого: %d", w.Code)
	}
	// пользователи сохраняются на диск
	m2, err := envServerMode(m.dir)
	if err != nil || len(m2.users) != 1 {
		t.Fatalf("перечитать users.json: %v %d", err, len(m2.users))
	}
	if _, ok := m2.checkPassword("admin", "password2"); !ok {
		t.Fatal("пароль после перезапуска")
	}
}

func TestServerShopPerBrowser(t *testing.T) {
	_, h := newTestServerMode(t)
	c := login(t, h, "admin", "password1")
	shops := func(path string) (active string, ids []string) {
		var st struct {
			Active string
			Shops  []struct{ ID string }
		}
		json.Unmarshal(do(h, "GET", path, "", c, nil).Body.Bytes(), &st)
		for _, s := range st.Shops {
			ids = append(ids, s.ID)
		}
		return st.Active, ids
	}
	first, _ := shops("/api/state")
	if w := do(h, "POST", "/api/profile/new", `{"demo":true,"name":"Второй"}`, c, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	// Store.Add делает новый магазин выбранным «по умолчанию»,
	// но браузер, который просит свой магазин, получает свой.
	def, ids := shops("/api/state")
	if len(ids) != 2 || def == first {
		t.Fatalf("магазины: %v, по умолчанию %s", ids, def)
	}
	if a, _ := shops("/api/state?id=" + first); a != first {
		t.Fatalf("выбранный магазин не учтён: %s", a)
	}
	if a, _ := shops("/api/state?id=nope"); a != def {
		t.Fatalf("несуществующий магазин: %s", a)
	}
}

func TestServerNoUsers(t *testing.T) {
	t.Setenv("OZON_RFBS_ADDR", "127.0.0.1:0")
	t.Setenv("OZON_RFBS_ADMIN_USER", "")
	t.Setenv("OZON_RFBS_ADMIN_PASSWORD", "")
	if _, err := envServerMode(t.TempDir()); err == nil {
		t.Fatal("без пользователей сервер не должен запускаться")
	}
}

func TestPBKDF2Vector(t *testing.T) {
	// RFC 7914, раздел 11: PBKDF2-HMAC-SHA256("passwd", "salt", 1, 64) — первые 32 байта.
	got := pbkdf2SHA256([]byte("passwd"), []byte("salt"), 1)
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc"
	if hexs := strings.ToLower(hexEnc(got)); hexs != want {
		t.Fatalf("pbkdf2: %s", hexs)
	}
}

func hexEnc(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[i*2], out[i*2+1] = d[x>>4], d[x&15]
	}
	return string(out)
}
