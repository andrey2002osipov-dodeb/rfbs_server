package main

// Серверный режим (rfbs_server): программа работает на сервере (OpenMediaVault, Docker),
// интерфейс открывается с компьютеров и телефонов по сети (через Tailscale).
//
// Включается переменной OZON_RFBS_ADDR (например 0.0.0.0:8766). Без неё программа
// ведёт себя как настольная версия: 127.0.0.1, без входа, открывает браузер.
//
// В серверном режиме:
//   - вход по логину и паролю (пользователи в users.json, пароли — PBKDF2-SHA256 с солью);
//   - магазины общие для всех пользователей, выбранный магазин у каждого свой (в браузере);
//   - кнопки «Закрыть программу» и «Открыть папку» отключены;
//   - защита от запросов с чужих сайтов: Origin должен совпадать с адресом сервера.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Сборка сервера: подставляется из файла SERVER_VERSION (-X main.serverVersion=...).
var serverVersion = "dev"

// srv — настройки серверного режима; nil — настольный режим.
var srv *serverMode

type serverMode struct {
	addr   string
	dir    string
	secret []byte

	mu    sync.Mutex
	users map[string]*userRec

	failMu sync.Mutex
	fails  map[string]*failRec
}

type userRec struct {
	Name    string    `json:"name"`
	Salt    string    `json:"salt"`
	Hash    string    `json:"hash"`
	Iter    int       `json:"iter"`
	Created time.Time `json:"created"`
}

type failRec struct {
	n     int
	first time.Time
}

const (
	sessionCookie = "rfbs_session"
	sessionTTL    = 60 * 24 * time.Hour
	pbkdfIter     = 120000
)

// envServerMode читает переменные окружения. Возвращает nil, если серверный режим не включён.
func envServerMode(dir string) (*serverMode, error) {
	addr := strings.TrimSpace(os.Getenv("OZON_RFBS_ADDR"))
	if addr == "" {
		return nil, nil
	}
	m := &serverMode{addr: addr, dir: dir, users: map[string]*userRec{}, fails: map[string]*failRec{}}
	if err := m.loadSecret(); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "users.json")); err == nil {
		var list []*userRec
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("users.json: %v", err)
		}
		for _, u := range list {
			m.users[strings.ToLower(u.Name)] = u
		}
	}
	// Первый пользователь — из переменных окружения (только если пользователей ещё нет).
	if len(m.users) == 0 {
		name := strings.TrimSpace(os.Getenv("OZON_RFBS_ADMIN_USER"))
		pass := os.Getenv("OZON_RFBS_ADMIN_PASSWORD")
		if name == "" || pass == "" {
			return nil, fmt.Errorf("нет ни одного пользователя: задайте OZON_RFBS_ADMIN_USER и OZON_RFBS_ADMIN_PASSWORD")
		}
		if err := m.setUser(name, pass, true); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *serverMode) loadSecret() error {
	p := filepath.Join(m.dir, "session.key")
	if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
		m.secret = b
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	m.secret = b
	return writeFileAtomic(p, b)
}

// ---------------- пароли ----------------

func pbkdf2SHA256(pass, salt []byte, iter int) []byte {
	mac := hmac.New(sha256.New, pass)
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iter; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}

func validUserName(n string) error {
	if n == "" || len(n) > 40 {
		return fmt.Errorf("логин: от 1 до 40 символов")
	}
	for _, r := range n {
		if r <= ' ' || r == ':' || r == '|' {
			return fmt.Errorf("логин не должен содержать пробелы и символы : |")
		}
	}
	return nil
}

// setUser создаёт пользователя или меняет ему пароль. create=true — только создание.
func (m *serverMode) setUser(name, pass string, create bool) error {
	name = strings.TrimSpace(name)
	if err := validUserName(name); err != nil {
		return err
	}
	if len([]rune(pass)) < 8 {
		return fmt.Errorf("пароль: не короче 8 символов")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	m.mu.Lock()
	defer m.mu.Unlock()
	k := strings.ToLower(name)
	old, exists := m.users[k]
	if create && exists {
		return fmt.Errorf("пользователь «%s» уже есть", name)
	}
	if !create && !exists {
		return fmt.Errorf("пользователь не найден")
	}
	u := &userRec{Name: name, Salt: hex.EncodeToString(salt), Iter: pbkdfIter, Created: time.Now()}
	if exists {
		u.Name, u.Created = old.Name, old.Created
	}
	u.Hash = hex.EncodeToString(pbkdf2SHA256([]byte(pass), salt, pbkdfIter))
	m.users[k] = u
	return m.saveUsersLocked()
}

func (m *serverMode) deleteUser(name, self string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := strings.ToLower(name)
	if _, ok := m.users[k]; !ok {
		return fmt.Errorf("пользователь не найден")
	}
	if k == strings.ToLower(self) {
		return fmt.Errorf("нельзя удалить самого себя")
	}
	if len(m.users) <= 1 {
		return fmt.Errorf("нельзя удалить последнего пользователя")
	}
	delete(m.users, k)
	return m.saveUsersLocked()
}

func (m *serverMode) saveUsersLocked() error {
	var list []*userRec
	for _, u := range m.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	b, _ := json.MarshalIndent(list, "", "  ")
	return writeFileAtomic(filepath.Join(m.dir, "users.json"), b)
}

func (m *serverMode) checkPassword(name, pass string) (*userRec, bool) {
	m.mu.Lock()
	u := m.users[strings.ToLower(strings.TrimSpace(name))]
	m.mu.Unlock()
	if u == nil {
		// Тратим столько же времени, чтобы не подсказывать, есть ли такой логин.
		pbkdf2SHA256([]byte(pass), []byte("0000000000000000"), pbkdfIter)
		return nil, false
	}
	salt, _ := hex.DecodeString(u.Salt)
	want, _ := hex.DecodeString(u.Hash)
	got := pbkdf2SHA256([]byte(pass), salt, u.Iter)
	return u, subtle.ConstantTimeCompare(got, want) == 1
}

func (m *serverMode) userList() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []map[string]any
	for _, u := range m.users {
		out = append(out, map[string]any{"name": u.Name, "created": u.Created})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]["name"].(string)) < strings.ToLower(out[j]["name"].(string))
	})
	return out
}

// ---------------- сессии ----------------
// Cookie: base64(логин|срок|отпечаток пароля)|подпись. Смена пароля или удаление
// пользователя сразу завершают его сессии на всех устройствах.

func (m *serverMode) sign(s string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (m *serverMode) newSession(u *userRec) string {
	exp := time.Now().Add(sessionTTL).Unix()
	payload := u.Name + "|" + strconv.FormatInt(exp, 10) + "|" + u.Hash[:16]
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return p + "." + m.sign(p)
}

// sessionUser — имя пользователя из cookie или "".
func (m *serverMode) sessionUser(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	p, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(m.sign(p))) {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return ""
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return ""
	}
	exp, _ := strconv.ParseInt(parts[1], 10, 64)
	if time.Now().Unix() > exp {
		return ""
	}
	m.mu.Lock()
	u := m.users[strings.ToLower(parts[0])]
	m.mu.Unlock()
	if u == nil || len(u.Hash) < 16 || u.Hash[:16] != parts[2] {
		return ""
	}
	return u.Name
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (m *serverMode) setCookie(w http.ResponseWriter, r *http.Request, val string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: val, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
}

// ---------------- защита от подбора пароля ----------------

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (m *serverMode) blocked(ip string) bool {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	f := m.fails[ip]
	if f == nil {
		return false
	}
	if time.Since(f.first) > 15*time.Minute {
		delete(m.fails, ip)
		return false
	}
	return f.n >= 10
}

func (m *serverMode) fail(ip string) {
	m.failMu.Lock()
	defer m.failMu.Unlock()
	f := m.fails[ip]
	if f == nil || time.Since(f.first) > 15*time.Minute {
		f = &failRec{first: time.Now()}
		m.fails[ip] = f
	}
	f.n++
}

// ---------------- проверка запросов ----------------

// sameOrigin — запрос пришёл со страницы этого же сервера (защита от CSRF).
func sameOrigin(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s == "cross-site" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return o == ""
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	// За прокси (tailscale serve, nginx) адрес, который видит браузер, приходит в X-Forwarded-Host.
	for _, h := range []string{r.Host, r.Header.Get("X-Forwarded-Host")} {
		if h != "" && strings.EqualFold(u.Host, h) {
			return true
		}
	}
	return false
}

// apiRequestAllowed — общая проверка для всех /api/ (вызывается из api.go).
func apiRequestAllowed(r *http.Request) bool {
	if srv == nil {
		// Настольный режим — как в исходной программе: только 127.0.0.1 и только своя страница.
		if h := r.Host; !strings.HasPrefix(h, "127.0.0.1:") && !strings.HasPrefix(h, "localhost:") {
			return false
		}
		if o := r.Header.Get("Origin"); o != "" && !strings.HasPrefix(o, "http://127.0.0.1:") && !strings.HasPrefix(o, "http://localhost:") {
			return false
		}
		return true
	}
	return sameOrigin(r)
}

// Пути, доступные без входа: страница входа и то, что ей нужно, PWA-файлы, проверка жизни.
var publicPaths = map[string]bool{
	"/login": true, "/api/login": true, "/api/ping": true,
	"/style.css": true, "/mobile.css": true, "/icon.svg": true,
	"/manifest.webmanifest": true, "/sw.js": true,
	"/icon-192.png": true, "/icon-512.png": true, "/apple-touch-icon.png": true,
}

// wrap — проверка входа перед всеми остальными обработчиками.
func (m *serverMode) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if publicPaths[r.URL.Path] || m.sessionUser(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]any{"error": "Нужно войти заново", "login": true})
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// routes — вход, выход, пользователи.
func (m *serverMode) routes(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if m.sessionUser(r) != "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		b, err := staticFS.ReadFile("static/login.html")
		if err != nil {
			http.Error(w, "нет страницы входа", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		reply := func(code int, v any) { w.WriteHeader(code); json.NewEncoder(w).Encode(v) }
		if r.Method != http.MethodPost || !sameOrigin(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			reply(403, map[string]any{"error": "запрос отклонён"})
			return
		}
		ip := clientIP(r)
		if m.blocked(ip) {
			reply(429, map[string]any{"error": "Слишком много неудачных попыток. Подождите 15 минут."})
			return
		}
		var b struct{ User, Password string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		u, ok := m.checkPassword(b.User, b.Password)
		if !ok {
			m.fail(ip)
			appLogf("неудачный вход: логин %q, адрес %s", b.User, ip)
			time.Sleep(400 * time.Millisecond)
			reply(401, map[string]any{"error": "Неверный логин или пароль"})
			return
		}
		m.setCookie(w, r, m.newSession(u), int(sessionTTL/time.Second))
		appLogf("вход: %s, адрес %s", u.Name, ip)
		reply(200, map[string]any{"ok": true, "user": u.Name})
	})

	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodPost || !sameOrigin(r) {
			w.WriteHeader(403)
			return
		}
		m.setCookie(w, r, "", -1)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	s.handle(mux, "/api/users", func(r *http.Request, b map[string]any) (any, error) {
		return map[string]any{"users": m.userList(), "me": m.sessionUser(r)}, nil
	})
	s.handle(mux, "/api/users/add", func(r *http.Request, b map[string]any) (any, error) {
		if err := m.setUser(str(b, "name"), rawStr(b, "password"), true); err != nil {
			return nil, err
		}
		appLogf("%s добавил пользователя %s", m.sessionUser(r), str(b, "name"))
		return map[string]any{"ok": true}, nil
	})
	// Свой обработчик: после смены своего пароля нужно выдать новую cookie.
	mux.HandleFunc("/api/users/password", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost || !sameOrigin(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			w.WriteHeader(403)
			return
		}
		b := map[string]any{}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
		me := m.sessionUser(r)
		name := str(b, "name")
		if name == "" {
			name = me
		}
		if err := m.setUser(name, rawStr(b, "password"), false); err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			return
		}
		if strings.EqualFold(name, me) {
			m.refreshCookie(w, r, me)
		}
		appLogf("%s сменил пароль пользователю %s", me, name)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	s.handle(mux, "/api/users/delete", func(r *http.Request, b map[string]any) (any, error) {
		if err := m.deleteUser(str(b, "name"), m.sessionUser(r)); err != nil {
			return nil, err
		}
		appLogf("%s удалил пользователя %s", m.sessionUser(r), str(b, "name"))
		return map[string]any{"ok": true}, nil
	})
}

// После смены своего пароля текущая cookie становится недействительной —
// выдаём новую, чтобы пользователя не выкинуло.
func (m *serverMode) refreshCookie(w http.ResponseWriter, r *http.Request, name string) {
	m.mu.Lock()
	u := m.users[strings.ToLower(name)]
	m.mu.Unlock()
	if u != nil {
		m.setCookie(w, r, m.newSession(u), int(sessionTTL/time.Second))
	}
}

func rawStr(b map[string]any, k string) string {
	v, _ := b[k].(string)
	return v
}

// ---------------- журнал программы ----------------

var appLogf = func(format string, a ...any) {}
