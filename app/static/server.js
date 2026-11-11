'use strict';
// Дополнения серверной версии (rfbs_server): вход и пользователи, PWA, подписи ячеек для мобильной вёрстки.
// Основной интерфейс — app.js (программа автора), здесь только то, чего в нём нет.

(() => {
  // ---------- подписи ячеек: на телефоне таблицы показываются карточками ----------
  // Каждой ячейке ставится data-label из заголовка столбца; mobile.css выводит его перед значением.
  function labelTable(table) {
    const heads = $$('thead th', table).map(th => th.textContent.trim());
    $$('tbody tr', table).forEach(tr => {
      [...tr.children].forEach((td, i) => { if (heads[i] && td.dataset.label !== heads[i]) td.dataset.label = heads[i]; });
    });
  }
  function watch(root) {
    const run = () => $$('table.grid', root).forEach(labelTable);
    new MutationObserver(run).observe(root, { childList: true, subtree: true });
    run();
  }
  ['#batchTable', '#ordersTable', '#skipTable', '#tplList'].forEach(sel => {
    const el = $(sel);
    if (el) watch(el.tagName === 'TABLE' ? el.parentElement : el);
  });

  // ---------- PWA ----------
  if ('serviceWorker' in navigator && window.isSecureContext) {
    navigator.serviceWorker.register('sw.js').catch(() => { });
  }

  // ---------- серверный режим ----------
  let applied = false;
  async function applyServer() {
    if (!S || !S.server) return;
    document.body.classList.add('server');
    $('#verFoot').textContent = `Ozon rFBS · версия ${S.version} · сервер ${S.server_version}`;
    if (applied) return;
    applied = true;
    $('#quitBtn').classList.add('hidden');
    $('#logoutBtn').classList.remove('hidden');
    $('#dOpen').classList.add('hidden');
    $('#shopsTitle').textContent = 'Магазины на сервере';
    $('#dataSub').textContent = 'Ключи, настройки, журналы и логи хранятся на сервере в этой папке контейнера (общая папка OpenMediaVault). Обновление контейнера их не трогает.';
    $('#dataNote').textContent = 'Программа работает на сервере постоянно, закрывать её не нужно. Включённая автоматизация продолжает работу после перезагрузки сервера или обновления контейнера. Не запускайте автоматизацию того же магазина ещё и на компьютере — запросы будут дублироваться.';
    $('#usersCard').classList.remove('hidden');
    loadUsers();
  }

  // app.js обновляет S в loadState — ловим момент, когда пришло состояние.
  const t = setInterval(() => { if (S && S.version) { applyServer(); if (applied || !S.server) clearInterval(t); } }, 200);

  $('#logoutBtn').onclick = async () => {
    if (!(await confirmBox('<h3>Выйти?</h3><p>Автоматизация на сервере продолжит работать.</p>', 'Выйти'))) return;
    await fetch('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }).catch(() => { });
    location.href = '/login';
  };

  // ---------- пользователи ----------
  async function loadUsers() {
    try {
      const j = await api('/api/users');
      $('#meName').textContent = j.me;
      $('#usersList').innerHTML = j.users.map(u => `<div class="shoprow">
        <span class="n">${esc(u.name)}</span>${u.name.toLowerCase() === j.me.toLowerCase() ? '<span class="chip next">это вы</span>' : ''}
        <span class="a"><button class="btn ghost sm" data-upw="${esc(u.name)}">Сменить пароль</button>
        ${u.name.toLowerCase() === j.me.toLowerCase() ? '' : `<button class="btn danger sm" data-udel="${esc(u.name)}">Удалить</button>`}</span></div>`).join('');
    } catch (e) { toast(e.message, true); }
  }

  function userForm(title, withName) {
    return new Promise(res => {
      $('#modalBody').innerHTML = `<h3>${title}</h3>
        ${withName ? '<label class="fld">Логин<input id="uName" class="full" autocomplete="off" autocapitalize="none"></label>' : ''}
        <label class="fld" style="margin-top:10px">Пароль (не короче 8 символов)<input id="uPass" class="full" type="password" autocomplete="new-password"></label>
        <label class="fld" style="margin-top:10px">Пароль ещё раз<input id="uPass2" class="full" type="password" autocomplete="new-password"></label>`;
      $('#modalYes').textContent = 'Сохранить'; $('#modalYes').className = 'btn primary';
      $('#modal').classList.remove('hidden');
      ($('#uName') || $('#uPass')).focus();
      const done = v => { $('#modal').classList.add('hidden'); $('#modalYes').onclick = $('#modalNo').onclick = null; res(v); };
      $('#modalNo').onclick = () => done(null);
      $('#modalYes').onclick = () => {
        const p = $('#uPass').value;
        if (p !== $('#uPass2').value) return toast('Пароли не совпадают', true);
        if (p.length < 8) return toast('Пароль не короче 8 символов', true);
        done({ name: withName ? $('#uName').value.trim() : '', password: p });
      };
    });
  }

  $('#userAdd').onclick = async () => {
    const f = await userForm('Новый пользователь', true);
    if (!f) return;
    try { await api('/api/users/add', f); toast('Пользователь «' + f.name + '» добавлен'); loadUsers(); } catch (e) { toast(e.message, true); }
  };
  $('#usersList').onclick = async e => {
    const pw = e.target.dataset.upw, del = e.target.dataset.udel;
    if (pw) {
      const f = await userForm('Новый пароль для «' + esc(pw) + '»', false);
      if (!f) return;
      try { await api('/api/users/password', { name: pw, password: f.password }); toast('Пароль изменён. Другие устройства этого пользователя выйдут из программы.'); } catch (er) { toast(er.message, true); }
    }
    if (del) {
      if (!(await confirmBox(`<h3>Удалить пользователя «${esc(del)}»?</h3><p>Он больше не сможет войти. Магазины и их данные не изменятся.</p>`, 'Удалить', true))) return;
      try { await api('/api/users/delete', { name: del }); loadUsers(); } catch (er) { toast(er.message, true); }
    }
  };
})();
