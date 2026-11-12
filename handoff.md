# Handoff: rfbs_server

## Цель

Развернуть программу **Ozon rFBS 1.5.2** ([digushok/rfbs](https://github.com/digushok/rfbs/releases/tag/v1.5.2)) на домашнем сервере OpenMediaVault, чтобы она работала круглосуточно. Сейчас это Windows-программа (`.exe`) с локальным веб-интерфейсом на `127.0.0.1:8766`.

Пользователь выбрал:

- **Доступ** — через Tailscale (VPN).
- **Мобильная версия** — весь функционал на телефоне.
- **Образ** — собирать в GitHub (GHCR), репозиторий публичный.
- **Данные** — начать с нуля, без переноса с Windows.
- **Магазины** — общие для всех пользователей, у каждого магазина свои данные, как в Windows-версии.

## Сервер

| Параметр | Значение |
|---|---|
| ОС | OpenMediaVault 8.1.5 (Synchrony), Debian 13, ядро 7.0.7 amd64 |
| Процессор | Intel Pentium Dual E2180 (старый x86-64) |
| Имя | `kucha` |
| Адрес в домашней сети | `192.168.1.88` |
| Адрес в Tailscale | `100.124.164.80` |
| Tailscale | Работает **в Docker-контейнере `tailscale`** (`tailscale/tailscale:latest`, `network_mode: host`, состояние в `/var/lib/tailscale` на томе). На самом хосте команды `tailscale` нет. Команды запускаются как `docker exec tailscale tailscale …` |
| Tailscale serve | Включён: `https://kucha.tailc40790.ts.net/` → `http://192.168.1.88:8766` |

## Текущее состояние

- ✅ Код готов и запушен в ветку `claude/zealous-mayer-9k2gv9`. Сейчас это единственная ветка репозитория, поэтому GitHub считает её основной.
- ✅ GitHub Actions (`.github/workflows/docker.yml`) зелёный: gofmt, vet, тесты, сборка образа, запуск контейнера и проверка входа.
- ✅ Образ публичный, скачивается без авторизации: `ghcr.io/andrey2002osipov-dodeb/rfbs_server:latest`. Есть также теги `1.5.2-s1` и `sha-<коммит>`.
- ✅ Контейнер `rfbs` на OMV запущен через плагин Compose, пользователь вошёл в программу.
- ✅ Tailscale serve настроен на HTTPS-адрес.
- ⏳ Устройства пользователя ещё не в Tailscale. Компьютер не установлен, телефон (OnePlus 9 Pro) был offline 23 дня. Поэтому `kucha.tailc40790.ts.net` у пользователя пока не открывается. Из дома программа доступна по `http://192.168.1.88:8766`.
- ⏳ Вход через HTTPS-адрес Tailscale (`tailscale serve` → программа) **не проверен вживую**. Проверка Origin учитывает `X-Forwarded-Host`, это покрыто тестом. Если будет 403 или «запрос отклонён», искать причину в `sameOrigin()` в `app/server.go`.
- ⏳ Пользователь ещё не завёл реальный магазин: Client-Id и API-ключ, склад, партии. Нужно пройти тестовый режим, потом боевой.
- ⏳ Не сделано «Disable key expiry» для `kucha` в панели Tailscale.
- ⏳ Не проверено, как выглядит интерфейс на реальном телефоне. Проверено только в эмуляции iPhone 13 через Playwright.

## Файлы

```
app/                         исходники rfbs 1.5.2 (первый коммит — без изменений) + серверные правки
  server.go          (новый) серверный режим: пользователи, PBKDF2, сессии, вход/выход, CSRF, анти-подбор
  server_test.go     (новый) тесты входа, сессий, пользователей, выбора магазина, вектор PBKDF2
  main.go            (правка) OZON_RFBS_ADDR, лог в stdout, wrap(), SIGTERM, no-cache для статики
  api.go             (правка) apiRequestAllowed(), /api/state?id=, поля server/user, quit/open-dir выкл.
  store.go           (правка) dataDir() из OZON_RFBS_DATA
  static/index.html  (правка) manifest/PWA-мета, mobile.css, server.js, иконки вкладок, выход, «Пользователи»
  static/app.js      (правка) 401 → /login, выбранный магазин в localStorage, текст обновления для сервера
  static/server.js   (новый) выход, пользователи, data-label для карточек, регистрация SW
  static/mobile.css  (новый) вёрстка ≤760px, нижняя панель вкладок, страница входа
  static/login.html  (новый) страница входа
  static/manifest.webmanifest, sw.js, icon-192.png, icon-512.png, apple-touch-icon.png  (новые) PWA
Dockerfile                   golang:1.22-alpine → alpine:3.20, su-exec, healthcheck /api/ping
docker/entrypoint.sh         chown /data на PUID:PGID, запуск не от root
docker-compose.yml           для OMV: образ, порт 8766, ./data:/data, ADMIN_USER/PASSWORD, PUID 1000/PGID 100
.github/workflows/docker.yml тесты + сборка + push в GHCR + проверка запуска контейнера
SERVER_VERSION               номер сборки сервера (1)
README.md                    обзор, переменные окружения, как обновлять с апстрима
docs/OMV.md                  пошаговая установка на OMV, Tailscale, телефон, обновление, неполадки
docs/CHANGES.md              точный список правок в файлах автора, чтобы переносить новые версии
```

## Что изменилось (по сути)

- **Серверный режим** включается переменной `OZON_RFBS_ADDR`. Без неё программа работает как исходная настольная, тесты автора проходят без изменений.
- **Пользователи** хранятся в `users.json`, пароли — PBKDF2-SHA256 со 120k итераций, реализован вручную, чтобы остаться на go 1.22 без зависимостей.
  - Первый пользователь создаётся из `OZON_RFBS_ADMIN_USER` и `OZON_RFBS_ADMIN_PASSWORD`, только пока пользователей нет. Пароль не короче 8 символов.
  - Все пользователи равноправны: могут добавлять и удалять других и менять им пароли. Удалить себя или последнего пользователя нельзя.
- **Сессии** — подписанная HMAC cookie `rfbs_session` на 60 дней, секрет лежит в `session.key`. В подпись входит отпечаток хеша пароля, поэтому смена пароля или удаление пользователя сразу завершают его сессии. Сменившему свой пароль выдаётся новая cookie.
- **Защита:**
  - вместо проверки «только 127.0.0.1» — проверка Origin против Host или X-Forwarded-Host, а также Sec-Fetch-Site;
  - SameSite=Lax, Secure за HTTPS;
  - 10 неверных паролей за 15 минут с одного IP — блокировка.
- **Выбранный магазин хранится в браузере** (`localStorage` → `/api/state?id=`). Исходно он был глобальный (`Store.Active`), и при нескольких пользователях один переключал магазин другому.
- **Мобильная вёрстка:**
  - нижняя панель вкладок;
  - таблицы превращаются в карточки: `server.js` ставит `data-label` из `<thead>` через MutationObserver, `mobile.css` выводит подписи;
  - модальные окна — снизу экрана, поля 16px, чтобы iOS не увеличивал страницу при вводе.
- **PWA:** манифест, иконки и service worker, который ничего не кэширует, только показывает страницу «нет связи». Установка на Android требует HTTPS, поэтому нужен `tailscale serve`.
- **`docker stop` (SIGTERM)** вызывает `mgr.StopAll()` без сброса `Profile.Running`, так что автоматизация продолжается после перезапуска.

## Что пробовал / что не сработало

| Что | Результат | Решение |
|---|---|---|
| Нижняя панель вкладок `position: fixed` внутри `.top` | Панель прилипла к шапке и перекрыла её: `backdrop-filter` у `.top` создаёт containing block для fixed-потомков | На мобильном у `.top` убран `backdrop-filter` |
| `.card-h > div { flex: 1 }` | Кнопки «Партии» вылезали за край, потому что `.actions` — тоже `div` | Селектор `.card-h > div:not(.actions)` |
| Правки в `static/` без пересборки | Изменения не видны: статика встроена через `go:embed` | После правок в `static/` бинарник нужно пересобирать |
| Локальный `docker build` в облачной среде | 429 от Docker Hub, `apk add` не проходит через прокси | Образ собирается и проверяется только в GitHub Actions |
| CI: `docker run --name t` | `Invalid container name (t)` | Имя `rfbs-check` |
| `sudo tailscale serve` на хосте | `tailscale: command not found` | Tailscale в контейнере: `docker exec tailscale tailscale serve --bg http://$(hostname -I \| awk '{print $1}'):8766` |
| Открыть `kucha.tailc40790.ts.net` с компьютера | «Сервер не найден» | Компьютер не в Tailscale. Нужно поставить Tailscale на компьютер и телефон. Из дома: `http://192.168.1.88:8766` |
| Контейнер `rfbs` в compose | `Restarting (1)` | Пароль в `OZON_RFBS_ADMIN_PASSWORD` был короче 8 символов. Предупреждение добавлено в compose и `docs/OMV.md` |
| Подключиться к OMV из облачной сессии | Невозможно: сервер только в домашней сети и Tailscale | Предложено поставить Claude Code на сам OMV (`claude remote-control` в tmux). Пользователь пока не ставил |

## Следующие шаги

1. Установить Tailscale на компьютер пользователя, включить на телефоне, проверить `https://kucha.tailc40790.ts.net/` и вход по нему. Если будет 403 — `sameOrigin()`.
2. Установить PWA на телефон и посмотреть все три вкладки на реальном устройстве.
3. Сменить пароль в программе, стереть пароль из compose-файла, добавить остальных пользователей.
4. Завести реальный магазин и пройти тестовый режим: окна доставки, время «Условно доставлен». Потом боевой режим. Windows-версию с тем же магазином не запускать одновременно.
5. «Disable key expiry» для `kucha` в Tailscale.
6. Новая версия апстрима: разложить поверх `app/`, сверить с `docs/CHANGES.md`, `go test ./...`, push. На OMV: Pull → Up.

## Полезные команды на сервере

```bash
docker ps -a --format '{{.Names}}  {{.Status}}' | grep rfbs
```

```bash
docker logs rfbs --tail 30
```

```bash
docker exec tailscale tailscale status
```

```bash
docker exec tailscale tailscale serve status
```

Локальный запуск для разработки:

```bash
cd app && OZON_RFBS_ADDR=127.0.0.1:8766 OZON_RFBS_DATA=./data-dev OZON_RFBS_ADMIN_USER=admin OZON_RFBS_ADMIN_PASSWORD=password1 go run .
```
