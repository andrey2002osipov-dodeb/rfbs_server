'use strict';
// Service worker серверной версии: нужен, чтобы программу можно было установить на телефон (PWA).
// Ничего не кэширует: данные о заказах всегда должны быть свежими. Без связи показывает понятную страницу.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', e => {
  if (e.request.mode !== 'navigate') return;
  e.respondWith(fetch(e.request).catch(() => new Response(
    '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">' +
    '<title>Нет связи</title><body style="font:16px system-ui;padding:32px;color:#141821;background:#f4f6fa">' +
    '<h2>Нет связи с сервером</h2><p>Проверьте, что на телефоне включён Tailscale и есть интернет, затем обновите страницу.</p>' +
    '<p><button onclick="location.reload()" style="font:inherit;padding:10px 18px;border:0;border-radius:10px;background:#0b6bf2;color:#fff">Обновить</button></p>',
    { headers: { 'Content-Type': 'text/html; charset=utf-8' } })));
});
