<script>
  import { onMount, onDestroy } from 'svelte'
  import {
    GetConfig, SaveConfig, TestTMDB, TestOneFichier,
    TestUsenet, HasSeedboxSettingsPassword, SetSeedboxSettingsPassword, VerifySeedboxSettingsPassword,
    ClearSeedboxSettingsPassword, UpdateMyElyNzb, UpdateMyElyDdl, GetElysiumMeta,
    BinariesStatus
  } from '../wailsjs/go/main/App.js'
  import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime.js'
  import HydrackerTab from './HydrackerTab.svelte'
  import { logEntries, addLog, clearLogs } from './logs.js'
  import logo from './assets/logo.png'
  import {
    GetVersion, StartWatchFolder, StopWatchFolder, CheckForUpdate,
    OpenBrowser, HistoryList, HistoryDelete, HistoryStats,
    DownloadUpdate, HasLihdlSettingsPassword, SetLihdlSettingsPassword, VerifyLihdlSettingsPassword,
    ClearLihdlSettingsPassword, IsLihdlPasswordManaged, IsHydrackerURLManaged, GetEffectiveHydrackerURL,
    LoginUser, FetchHydrackerAvatar, GetDDLFilenameByLienID, TestIgdb,
    TestElysium, UpdateMyLien, UpdateMyTorrent, GetMetaQualities,
    GetUserProfile, Notify, ListMyElyNzbs, ListMyElyDdls,
    DeleteMyElyNzb, DeleteMyElyDdl
  } from '../wailsjs/go/main/App.js'

  // --- Tabs (réorganisés par workflow, 8 onglets principaux) ---
  // Pictos SVG (tracés heroicons, stroke 1.6) plutôt que des emojis :
  // ils suivent la couleur du texte, restent nets à toutes les tailles et
  // ne dépendent pas de la police emoji du système.
  const TABS = [
    { id: 'hydracker', label: 'Publier',
      icon: 'M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 9 12 4.5 7.5 9M12 4.5V16.5' },
    // 'fiches', 'check', 'reseed' retirés — flow torrent + browsing supprimés
    { id: 'myuploads', label: 'Mes uploads',
      icon: 'M3.75 9.75h16.5M3.75 9.75 12 4.5l8.25 5.25M4.5 9.75v9a1.5 1.5 0 0 0 1.5 1.5h12a1.5 1.5 0 0 0 1.5-1.5v-9' },
    { id: 'history',   label: 'Historique',
      icon: 'M12 6.75V12l3.75 2.25M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z' },
    { id: 'logs',      label: 'Logs',           // fusion : Journal + API
      icon: 'M6.75 7.5l3 2.25-3 2.25m4.5 0h3M4.5 19.5h15a1.5 1.5 0 0 0 1.5-1.5V6a1.5 1.5 0 0 0-1.5-1.5h-15A1.5 1.5 0 0 0 3 6v12a1.5 1.5 0 0 0 1.5 1.5Z' },
    // 'manager' retiré : plus d'auth = plus de gestion utilisateurs
    { id: 'settings',  label: 'Réglages',
      icon: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM12 3v2.25M12 18.75V21M3 12h2.25M18.75 12H21M5.64 5.64l1.59 1.59M16.77 16.77l1.59 1.59M18.36 5.64l-1.59 1.59M7.23 16.77l-1.59 1.59' },
  ]
  let logsSubTab = 'journal'      // 'journal' | 'api'

  // Outils embarqués (parpar, nyuu, mediainfo) : l'app les contient, mais
  // il faut pouvoir le vérifier — un repli sur le PATH doit sauter aux yeux.
  let outils = []
  // Forme attendue d'une clé API 1Fichier. Un champ rempli avec autre chose
  // — une URL, une liste de liens collée par mégarde — passait inaperçu : le
  // test de clé validait tout, et l'envoi partait en anonyme, donc hors du
  // compte. On ne bloque pas la saisie, on la signale.
  const cleApparenteValide = k => /^[A-Za-z0-9_-]{20,80}$/.test(String(k || '').trim())

  // Comptes 1Fichier : on lie directement cfg.one_fichier_accounts. Un
  // tableau local recopié dans cfg créait une dépendance circulaire
  // (comptes1F → cfg → comptes1F), que Svelte refuse de compiler.
  $: if (activeTab === 'settings' && !outils.length) {
    BinariesStatus().then(r => outils = r || []).catch(() => {})
  }

  // --- Journal : filtres, recherche, copie ---
  // Le Journal sert à retrouver UNE ligne parmi des centaines quand un post
  // échoue. Les tags sont masquables individuellement (on part de tout
  // visible) et la recherche porte sur le tag comme sur le message.
  let logQuery = ''
  let logTagsOff = []
  let logCopied = false
  $: logTags = [...new Set($logEntries.map(e => e.prefix))].sort()
  $: logsShown = $logEntries.filter(e =>
       !logTagsOff.includes(e.prefix) &&
       (logQuery === '' || (e.prefix + ' ' + e.msg).toLowerCase().includes(logQuery.toLowerCase())))
  function toggleLogTag(t) {
    logTagsOff = logTagsOff.includes(t) ? logTagsOff.filter(x => x !== t) : [...logTagsOff, t]
  }
  // Un échec doit sauter aux yeux : on se fie aux marqueurs que posent les
  // appels addLog eux-mêmes plutôt qu'à un niveau de log qu'ils ne portent pas.
  const logSeverity = m =>
    /✗|échec|echec|erreur|error|impossible|482/i.test(m) ? 'err'
    : /⚠|warn|attention/i.test(m) ? 'warn' : ''
  async function copyLogs() {
    try {
      await navigator.clipboard.writeText(
        logsShown.map(e => `${e.ts} [${e.prefix}] ${e.msg}`).join('\n'))
      logCopied = true
      setTimeout(() => logCopied = false, 1500)
    } catch (e) { addLog('LOG', '✗ copie impossible : ' + e) }
  }
  let activeTab = 'hydracker'

  // Pictos SVG (jamais d'emoji dans l'interface). Constantes plutôt que
  // balisage répété : ces pictos reviennent à plusieurs endroits et doivent
  // rester identiques. Les tailles sont dans style.css, en global : une règle
  // scopée n'atteindrait pas les pictos injectés via {@html}.
  const _ico = d => `<svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor"`
    + ` stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="${d}"/></svg>`
  const ICO_ARCHIVE = _ico('M20.25 7.5l-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125Z')
  const ICO_LINK    = _ico('M13.19 8.688a4.5 4.5 0 0 1 1.242 7.244l-4.5 4.5a4.5 4.5 0 0 1-6.364-6.364l1.757-1.757m13.35-.622 1.757-1.757a4.5 4.5 0 0 0-6.364-6.364l-4.5 4.5a4.5 4.5 0 0 0 1.242 7.244')
  const ICO_NEWS    = _ico('M12 7.5h1.5m-1.5 3h1.5m-7.5 3h7.5m-7.5 3h7.5m3-9h3.375c.621 0 1.125.504 1.125 1.125V18a2.25 2.25 0 0 1-2.25 2.25M16.5 7.5V18a2.25 2.25 0 0 0 2.25 2.25M16.5 7.5V4.875c0-.621-.504-1.125-1.125-1.125H4.125C3.504 3.75 3 4.254 3 4.875V18a2.25 2.25 0 0 0 2.25 2.25h13.5M6 7.5h3v3H6v-3Z')
  const ICO_REFRESH = _ico('M4.5 12a7.5 7.5 0 0 1 12.8-5.3M19.5 12a7.5 7.5 0 0 1-12.8 5.3M17.3 3.5v3.2h-3.2M6.7 20.5v-3.2h3.2')
  const ICO_PEN     = _ico('M16.5 4.5l3 3L8.25 18.75 4.5 19.5l.75-3.75L16.5 4.5Z')
  const ICO_TRASH   = _ico('M9.75 9.75v7.5m4.5-7.5v7.5M4.5 6.75h15m-1.5 0-.7 12.1a1.5 1.5 0 0 1-1.5 1.4H8.2a1.5 1.5 0 0 1-1.5-1.4L6 6.75m3.75 0V5.25a1.5 1.5 0 0 1 1.5-1.5h1.5a1.5 1.5 0 0 1 1.5 1.5v1.5')
  const ICO_CAL     = _ico('M6.75 3v2.25M17.25 3v2.25M3 11.25h18M5.25 5.25h13.5A2.25 2.25 0 0 1 21 7.5v11.25A2.25 2.25 0 0 1 18.75 21H5.25A2.25 2.25 0 0 1 3 18.75V7.5a2.25 2.25 0 0 1 2.25-2.25Z')
  const ICO_WARN    = _ico('M12 9v3.75m0 3.5h.008M10.34 3.94 2.7 16.12A1.5 1.5 0 0 0 4 18.4h16a1.5 1.5 0 0 0 1.3-2.28L13.66 3.94a1.5 1.5 0 0 0-2.62 0Z')
  const ICO_CHECK   = _ico('m4.5 12.75 6 6 9-13.5')
  const ICO_SAVE    = _ico('M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M7.5 10.5 12 15m0 0 4.5-4.5M12 15V3')
  const ICO_SEARCH  = _ico('m21 21-4.35-4.35M17 11a6 6 0 1 1-12 0 6 6 0 0 1 12 0Z')
  const ICO_COPY    = _ico('M9 9V6.75A1.5 1.5 0 0 1 10.5 5.25h6.75A1.5 1.5 0 0 1 18.75 6.75v6.75a1.5 1.5 0 0 1-1.5 1.5H15M6.75 9h6.75a1.5 1.5 0 0 1 1.5 1.5v6.75a1.5 1.5 0 0 1-1.5 1.5H6.75a1.5 1.5 0 0 1-1.5-1.5V10.5A1.5 1.5 0 0 1 6.75 9Z')
  const ICO_TERMINAL= _ico('M6.75 7.5l3 2.25-3 2.25m4.5 0h3M4.5 19.5h15a1.5 1.5 0 0 0 1.5-1.5V6a1.5 1.5 0 0 0-1.5-1.5h-15A1.5 1.5 0 0 0 3 6v12a1.5 1.5 0 0 0 1.5 1.5Z')
  const ICO_LIST    = _ico('M8.25 6.75h10.5M8.25 12h10.5M8.25 17.25h10.5M4.5 6.75h.008M4.5 12h.008M4.5 17.25h.008')
  const ICO_HISTORY = _ico('M12 6.75V12l3.75 2.25M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z')
  const ICO_CROSS   = _ico('M6 18 18 6M6 6l12 12')
  const ICO_KEY     = _ico('M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.03 5.91l-1.47 1.47a2.25 2.25 0 0 1-1.59.66H10.5v1.5a1.5 1.5 0 0 1-1.5 1.5H7.5v1.5a1.5 1.5 0 0 1-1.5 1.5H4.5a1.5 1.5 0 0 1-1.5-1.5v-1.65c0-.6.24-1.17.66-1.59l5.52-5.52A6 6 0 1 1 21.75 8.25Z')
  const ICO_COG     = _ico('M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM12 3v2.25M12 18.75V21M3 12h2.25M18.75 12H21M5.64 5.64l1.59 1.59M16.77 16.77l1.59 1.59M18.36 5.64l-1.59 1.59M7.23 16.77l-1.59 1.59')
  const ICO_LOCK    = _ico('M16.5 10.5V6.75a4.5 4.5 0 1 0-9 0v3.75M6.75 21.75h10.5a2.25 2.25 0 0 0 2.25-2.25v-6.75a2.25 2.25 0 0 0-2.25-2.25H6.75a2.25 2.25 0 0 0-2.25 2.25v6.75a2.25 2.25 0 0 0 2.25 2.25Z')
  const ICO_PLAY    = _ico('M5.25 5.65c0-.85.92-1.38 1.65-.96l11.54 6.6c.74.42.74 1.5 0 1.92l-11.54 6.6a1.1 1.1 0 0 1-1.65-.96V5.65Z')
  const ICO_STOP    = _ico('M6.75 6.75h10.5v10.5H6.75z')
  const ICO_UNLOCK  = _ico('M13.5 10.5V6.75a4.5 4.5 0 0 1 9 0v.75M3.75 21.75h10.5a2.25 2.25 0 0 0 2.25-2.25v-6.75a2.25 2.25 0 0 0-2.25-2.25H3.75a2.25 2.25 0 0 0-2.25 2.25v6.75a2.25 2.25 0 0 0 2.25 2.25Z')
  const ICO_DOC     = _ico('M19.5 14.25v-2.63c0-1.14-.9-2.06-2.01-2.06h-1.5a1.13 1.13 0 0 1-1.12-1.12v-1.5c0-1.11-.92-2.01-2.06-2.01H10.5M18 18.75h-5.25m-3-13.5H6.38c-.62 0-1.13.5-1.13 1.12v12.76c0 .62.5 1.12 1.12 1.12h11.25c.63 0 1.13-.5 1.13-1.12V11.25a6 6 0 0 0-6-6Z')
  const ICO_SPARK   = _ico('M9.8 3.3 11 6.4l3.1 1.2-3.1 1.2L9.8 12 8.6 8.8 5.5 7.6l3.1-1.2L9.8 3.3ZM17.5 13l.8 2 2 .8-2 .8-.8 2-.8-2-2-.8 2-.8.8-2Z')
  const ICO_DOWNLOAD= _ico('M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3')

  let myUsername = ''
  let myAvatar = ''


  async function fetchAvatar() {
    try {
      const av = await FetchHydrackerAvatar()
      if (av) myAvatar = av
    } catch {}
  }



  let teamUsers = []                     // [{ pseudo, role, title }]
  let teamRoles = {}                     // { [key]: { badge, color, title, tabs: [] } }




  // On référence teamRoles + teamUsers directement (pas via currentManagerSnapshot()),
  // sinon Svelte ne tracke pas les deps à travers l'appel de fonction et le bloc
  // ne re-run pas quand on toggle un tab/permission/user dans le Manager.
  // managerSaved = true tant que l'état actuel correspond exactement à ce qui a été
  // généré la dernière fois (= JSON copié dans le presse-papier). Auto-clear si l'user
  // continue à éditer après le "Générer team.json".

  function currentManagerSnapshot() {
    return { roles: teamRoles, users: teamUsers }
  }











  // Auto-load quand on ouvre l'onglet Manager



  // Les permissions par rôle sont maintenant définies dans team.json
  // (section "roles") et l'app reçoit la liste `tabs` via LoginUser.
  // (L'onglet Manager a été retiré avec l'authentification.)
  // Auth retirée : tous les onglets sont visibles (plus de rôle/permissions).
  $: visibleTabs = TABS

  // --- Config ---
  let cfg = {
    hydracker_token: '',
    tmdb_api_key: '',
    tmdb_proxy_url: 'https://tmdb.uklm.xyz',
    lihdl_base_url: '',
    one_fichier_api_key: '',
    one_fichier_accounts: [],
    one_fichier_folder: 'GO POST TOOLS',
    sendcm_api_key: '',
    nexum_api_key: '',
    nexum_base_url: 'https://nexum-core.com',
    alldebrid_api_key: '',
    igdb_client_id: '',
    igdb_client_secret: '',
    elysium_api_token: '',
    usenet_host: '', usenet_port: 119, usenet_ssl: false,
    usenet_user: '', usenet_password: '', usenet_connections: 20,
    usenet_group: 'alt.binaries.test',
    parpar_redundancy: 5, parpar_threads: 8, parpar_slice_size: 768000,
    ftp_host: '', ftp_port: 21, ftp_user: '', ftp_password: '', ftp_path: '', ftp_use_sftp: false,
    private_ftp_host: '', private_ftp_port: 21, private_ftp_user: '', private_ftp_password: '', private_ftp_path: '',
    seedbox_url: '', seedbox_user: '', seedbox_password: '', seedbox_label: '',
    private_seedbox_url: '', private_seedbox_user: '', private_seedbox_password: '', private_seedbox_label: '',
    private_qbit_url: '', private_qbit_user: '', private_qbit_password: '',
    qbit_url: '', qbit_user: '', qbit_password: '',
    qbit_admin_url: '', qbit_admin_user: '', qbit_admin_password: '',
    nextcloud_admin_url: '', nextcloud_admin_user: '', nextcloud_admin_password: '', nextcloud_admin_path: '/',
    mod_seedbox_url: '', mod_seedbox_user: '', mod_seedbox_password: '',
    ftp_mod_host: '', ftp_mod_port: 21, ftp_mod_user: '', ftp_mod_password: '', ftp_mod_path: '/',
    tracker_url: '', torrent_piece_size: 8388608,
    lihdl_user: '', lihdl_password: '',
    watch_folder: '', watch_auto_start: false,
    proxy_url: '',
  }
  let appVersion = ''
  let watchRunning = false
  let updateInfo = null  // { available, current, latest, url }
  let cfgSaved = false
  let sidebarCollapsed = false
  let updateChecking = false
  let updateCheckMsg = ''

  // LiHDL : protection admin (index + recherche TMDB — bonus pour les admins
  // qui ont le mdp partagé, les autres s'en passent et utilisent l'API TMDB).
  let lihdlUnlocked = false
  let lihdlHasPassword = false
  let lihdlManaged = false // true = mdp imposé au build, user ne peut pas le modifier
  let hydrackerURLManaged = false // true = URL API imposée au build
  let lihdlModal = null  // null | 'unlock' | 'create' | 'change' | 'remove'
  let lihdlPwdInput = ''
  let lihdlPwdCurrent = ''
  let lihdlPwdNew = ''
  let lihdlPwdConfirm = ''
  let lihdlPwdError = ''

  async function checkLihdlPasswordStatus() {
    try { lihdlHasPassword = await HasLihdlSettingsPassword() } catch(e) { lihdlHasPassword = false }
    try { lihdlManaged = await IsLihdlPasswordManaged() } catch(e) { lihdlManaged = false }
  }

  // --- Sections SEEDBOX/FTP : plus de mdp, toujours ouvert ---
  let seedboxUnlocked = true
  let seedboxHasPassword = false
  let seedboxModal = null  // null | 'unlock' | 'create' | 'change' | 'remove'
  let seedboxPwdInput = ''
  let seedboxPwdCurrent = ''
  let seedboxPwdNew = ''
  let seedboxPwdConfirm = ''
  let seedboxPwdError = ''

  async function checkSeedboxPasswordStatus() {
    try { seedboxHasPassword = await HasSeedboxSettingsPassword() } catch(e) { seedboxHasPassword = false }
  }


  async function submitSeedboxPwd() {
    seedboxPwdError = ''
    try {
      if (seedboxModal === 'unlock') {
        const ok = await VerifySeedboxSettingsPassword(seedboxPwdInput)
        if (!ok) { seedboxPwdError = 'Mot de passe incorrect'; return }
        seedboxUnlocked = true
        seedboxModal = null
      } else if (seedboxModal === 'create') {
        if (seedboxPwdNew.length < 4) { seedboxPwdError = 'Mot de passe trop court (4 min)'; return }
        if (seedboxPwdNew !== seedboxPwdConfirm) { seedboxPwdError = 'Les mots de passe ne correspondent pas'; return }
        await SetSeedboxSettingsPassword('', seedboxPwdNew)
        seedboxHasPassword = true
        seedboxUnlocked = true
        seedboxModal = null
      } else if (seedboxModal === 'change') {
        if (seedboxPwdNew.length < 4) { seedboxPwdError = 'Mot de passe trop court (4 min)'; return }
        if (seedboxPwdNew !== seedboxPwdConfirm) { seedboxPwdError = 'Les mots de passe ne correspondent pas'; return }
        await SetSeedboxSettingsPassword(seedboxPwdCurrent, seedboxPwdNew)
        seedboxModal = null
      } else if (seedboxModal === 'remove') {
        await ClearSeedboxSettingsPassword(seedboxPwdCurrent)
        seedboxHasPassword = false
        seedboxModal = null
      }
    } catch(e) { seedboxPwdError = String(e).replace('Error: ', '') }
  }


  async function submitLihdlPwd() {
    lihdlPwdError = ''
    try {
      if (lihdlModal === 'unlock') {
        const ok = await VerifyLihdlSettingsPassword(lihdlPwdInput)
        if (!ok) { lihdlPwdError = 'Mot de passe incorrect'; return }
        lihdlUnlocked = true
        lihdlModal = null
      } else if (lihdlModal === 'create') {
        if (lihdlPwdNew.length < 4) { lihdlPwdError = 'Mot de passe trop court (4 min)'; return }
        if (lihdlPwdNew !== lihdlPwdConfirm) { lihdlPwdError = 'Les mots de passe ne correspondent pas'; return }
        await SetLihdlSettingsPassword('', lihdlPwdNew)
        lihdlHasPassword = true
        lihdlUnlocked = true
        lihdlModal = null
      } else if (lihdlModal === 'change') {
        if (lihdlPwdNew.length < 4) { lihdlPwdError = 'Mot de passe trop court (4 min)'; return }
        if (lihdlPwdNew !== lihdlPwdConfirm) { lihdlPwdError = 'Les mots de passe ne correspondent pas'; return }
        await SetLihdlSettingsPassword(lihdlPwdCurrent, lihdlPwdNew)
        lihdlModal = null
      } else if (lihdlModal === 'remove') {
        await ClearLihdlSettingsPassword(lihdlPwdCurrent)
        lihdlHasPassword = false
        lihdlModal = null
      }
    } catch(e) { lihdlPwdError = String(e).replace('Error: ', '') }
  }

  async function recheckUpdate() {
    updateChecking = true
    updateCheckMsg = ''
    addLog('UPDATE', '→ Vérification des mises à jour…')
    try {
      updateInfo = await CheckForUpdate()
      addLog('UPDATE', `GitHub: v${updateInfo?.latest || '?'} · Local: v${updateInfo?.current || '?'} · Dispo: ${updateInfo?.available}`)
      if (updateInfo?.available) {
        updateCheckMsg = `v${updateInfo.latest} dispo`
      } else {
        updateCheckMsg = `✓ À jour (v${updateInfo?.current || ''})`
        setTimeout(() => { updateCheckMsg = '' }, 5000)
      }
    } catch(e) {
      updateCheckMsg = '✗ Erreur'
      addLog('UPDATE', `✗ Erreur check maj: ${e}`)
      setTimeout(() => { updateCheckMsg = '' }, 8000)
    }
    updateChecking = false
  }

  // Update modal
  let showUpdateModal = false
  let updateState = { stage: '', msg: '', percent: 0, downloading: false, downloadedPath: '' }

  async function startDownloadUpdate() {
    updateState = { stage: '', msg: '', percent: 0, downloading: true, downloadedPath: '' }
    try {
      const path = await DownloadUpdate()
      updateState = { ...updateState, downloading: false, downloadedPath: path, stage: 'done' }
    } catch(e) {
      updateState = { ...updateState, downloading: false, stage: 'error', msg: String(e) }
      addLog('UPDATE', '✗ ' + e)
    }
  }

  // Historique
  let histEntries = []
  let histFilter = ''      // '' | 'torrent' | 'nzb' | 'ddl'
  let histQuery = ''
  let histStats = { total: 0, ok: 0, error: 0, torrent: 0, nzb: 0, ddl: 0 }
  let histLoading = false

  async function loadHistory() {
    histLoading = true
    try {
      histEntries = await HistoryList(histFilter, histQuery, 0, 500) || []
      histStats = await HistoryStats() || histStats
    } catch(e) { addLog('HIST', '✗ ' + e) }
    histLoading = false
  }
  $: if (activeTab === 'history') loadHistory()

  async function deleteHistEntry(id) {
    if (!confirm('Supprimer cette entrée ?')) return
    try { await HistoryDelete(id); await loadHistory() } catch(e) {}
  }

  function formatDate(iso) {
    if (!iso) return ''
    const d = new Date(iso)
    return d.toLocaleString('fr-FR', { day:'2-digit', month:'2-digit', year:'2-digit', hour:'2-digit', minute:'2-digit' })
  }

  // NZB live status (partagé avec l'onglet NZB)
  let nzbStatus = ''
  let nzbParparPct = 0
  let nzbNyuuPct = 0
  let nzbNyuuSpeed = ''
  let nzbNyuuETA = ''
  let nzbNyuuArticles = ''
  let nzbDone = false
  let nzbResult = null  // { ok, message }


  let reseedStage = ''
  let reseedMsg = ''
  let reseedPct = 0
  let reseedSpeed = 0



  let autoReseedStatus = ''           // dernier message du workflow



  // Variante DDL → FTP : quand la fiche n'a pas de torrent partagé via API mais
  // qu'un DDL 1fichier existe, on télécharge depuis le DDL et on pousse direct
  // sur le FTP configuré (streaming, pas de passage sur le disque local).
  let autoReseedDDLProgress = { percent: 0, speed_mb: 0, bytes: 0, total: 0 }



  // groupBySeason : transforme [{saison, episode, ...}] en [{ season:int, items:[], hasFullSeason:bool }]
  // Trié : saisons décroissantes, puis full_saison en haut, puis épisodes dans l'ordre.
  function groupBySeason(items) {
    const bySeason = new Map()
    for (const it of items || []) {
      const s = parseInt(it.saison || it.season || 0) || 0
      if (!bySeason.has(s)) bySeason.set(s, [])
      bySeason.get(s).push(it)
    }
    const seasons = [...bySeason.entries()]
      .map(([season, items]) => ({
        season,
        items: [...items].sort((a, b) => {
          // full_saison d'abord, puis par épisode croissant
          if (a.full_saison && !b.full_saison) return -1
          if (!a.full_saison && b.full_saison) return 1
          return (parseInt(a.episode||0)||0) - (parseInt(b.episode||0)||0)
        }),
      }))
      .sort((a, b) => b.season - a.season) // saisons décroissantes (dernière en haut)
    return seasons
  }








  // --- Modale NFO ---
  let nfoModal = null  // { kind, id, title, html, loading, error }
  function closeNfo() { nfoModal = null }

  // --- DDL filename resolver (cache mémoire pendant la session) ---
  // ddlFilenames[lien.id] = { state: 'loading'|'ok'|'err', filename, error }
  // Queue séquentielle keyée par lien ID (l'URL débridée change à chaque debrid).
  let ddlFilenames = {}
  let ddlQueue = []  // [lienId]
  let ddlQueueRunning = false
  async function processDDLQueue() {
    if (ddlQueueRunning) return
    ddlQueueRunning = true
    addLog('FICHE', `processDDLQueue start (${ddlQueue.length} en attente)`)
    while (ddlQueue.length > 0) {
      const lienId = ddlQueue.shift()
      addLog('FICHE', `→ debrid lien #${lienId}`)
      let attempt = 0
      while (attempt < 2) {
        try {
          const r = await GetDDLFilenameByLienID(lienId)
          addLog('FICHE', `← lien #${lienId} : url=${r?.url || '∅'} filename=${r?.filename || '∅'} (keys: ${Object.keys(r || {}).join(',')})`)
          ddlFilenames = { ...ddlFilenames, [lienId]: { state: 'ok', filename: r?.filename || '', url: r?.url || '' } }
          break
        } catch(e) {
          const msg = String(e)
          addLog('FICHE', `✗ lien #${lienId} : ${msg}`)
          if (attempt === 0 && /429|too many/i.test(msg)) {
            attempt++
            await new Promise(r => setTimeout(r, 2500))
            continue
          }
          ddlFilenames = { ...ddlFilenames, [lienId]: { state: 'err', error: msg } }
          break
        }
      }
      await new Promise(r => setTimeout(r, 1500))
    }
    ddlQueueRunning = false
    addLog('FICHE', `processDDLQueue done`)
  }
  // Auto-résolution batch dès qu'on bascule sur l'onglet "Liens DDL" d'une fiche.
  // On passe juste l'ID stable au backend qui se débrouille pour debrid + HEAD.
  // L'URL est volatile (chaque debrid en regénère une nouvelle) donc inutile
  // de la stocker côté frontend.


  let checkActionProg = {}          // { [torrentId]: {stage, percent, speed_mb, msg} }
  let checkActionActiveID = 0       // ID du torrent en cours d'action (pour router les events)



  // Index Nexum : map lowercase info_hash → {id, name, seeders, created_at, …}
  let nexumIndex = {}
  let nexumLoaded = false






  // normalizeName : strip . _ - espaces, lower → match robuste Hydracker ↔ Nexum
  // (les 2 trackers recalculent le hash différemment car ils remplacent l'announce)
  function normalizeNameJS(s) {
    if (!s) return ''
    return String(s).toLowerCase().replace(/[^a-z0-9]/g, '')
  }




  // nexumLookup : tente match par info_hash, fallback nom normalisé.
  // Retourne {seeders, leechers, id, name} ou null.
  function nexumLookup(t) {
    if (!nexumLoaded) return null
    const h = (t.info_hash || t.hash || '').toLowerCase()
    if (h) {
      const m = nexumIndex['h:' + h]
      if (m) return m
    }
    const name = t.torrent_name || t.name || ''
    if (name) {
      const m = nexumIndex['n:' + normalizeNameJS(name)]
      if (m) return m
    }
    return null
  }








  let reqProgress = {}              // { [requestId]: { stage, msg, percent, speed_mb, bytes, total } }
  let reqActiveID = 0               // ID de la demande en cours (pour router les events)


  // Reset page quand filtre change



// --- Mes uploads (admin CRUD sur tes propres items) ---
  // myUsername vient de l'auth team (résolu via /user-profile/me)
  let myTab = 'liens'                      // 'liens' | 'nzbs' (torrents retirés)
  let myItems = []                         // []Lien ou []TorrentItem
  let myLoading = false
  let myError = ''
  let myPage = 1
  let myTotalPages = 1
  let editingItem = null                   // item en cours d'édition (modal)
  let editPayload = { quality: '', language: [], subtitles: [] }
  let deletingItem = null                  // item dont on demande confirmation delete
  let deleteConfirmInput = ''              // texte tapé par user pour confirmer
  let qualityOptions = []

  // Adapte un UploadRef Elysium à la forme historique attendue par la UI
  // (mêmes noms de champs que renvoyait ex-Hydracker).
  function adaptElyUpload(u) {
    return {
      id: u.id,
      name: u.file_name,
      file_name: u.file_name,
      taille: u.size_bytes,
      size: u.size_bytes,
      qualite_name: u.quality || '',
      quality: u.quality || '',
      langues_compact: (u.language || []).map(n => ({ id: 0, name: n })),
      subs_compact:    (u.subtitles || []).map(n => ({ id: 0, name: n })),
      language:  u.language  || [],
      subtitles: u.subtitles || [],
      author: u.uploader?.name || '',
      created_at: u.created_at,
      title: u.title,
      tmdb_id: u.title?.tmdb_id || 0,
    }
  }

  // Tables de référence Elysium (qualités, langues, sous-titres) : ce sont
  // leurs CLÉS que le PATCH attend, pas les libellés Hydracker.
  let elyMeta = null
  async function chargerElyMeta() {
    if (elyMeta) return
    try { elyMeta = await GetElysiumMeta() } catch (e) { addLog('MY', '⚠ meta Elysium : ' + e) }
  }
  function basculer(liste, cle) {
    return liste.includes(cle) ? liste.filter(x => x !== cle) : [...liste, cle]
  }

  async function loadMyUploads() {
    myLoading = true
    myError = ''
    myItems = []
    try {
      // Pivot Elysium 2026-09-03 : Hydracker fermé. On source directement
      // depuis /api/v1/nzbs?mine=1 et /api/v1/ddls?mine=1 (paginés).
      let r
      if (myTab === 'liens') {
        r = await ListMyElyDdls(myPage)
      } else if (myTab === 'nzbs') {
        r = await ListMyElyNzbs(myPage)
      } else {
        r = null
      }
      if (r) {
        myItems = (r.data || []).map(adaptElyUpload)
        myTotalPages = r.meta?.last_page || 1
      }
    } catch(e) {
      myError = String(e?.message || e)
      addLog('MY', '✗ ' + myError)
    }
    myLoading = false
  }

  $: if (activeTab === 'myuploads') { loadMyUploads(); if (!qualityOptions.length) GetMetaQualities().then(q => qualityOptions = q || []).catch(() => {}) }
  $: if (myTab) { myPage = 1 }

  function startEdit(item) {
    editingItem = item
    // Préremplissage avec l'état actuel : ainsi enregistrer sans rien toucher
    // réécrit les mêmes valeurs au lieu d'en effacer.
    editPayload = {
      quality:   item.quality   || '',
      language:  [...(item.language  || [])],
      subtitles: [...(item.subtitles || [])],
    }
    chargerElyMeta()
  }
  function cancelEdit() { editingItem = null }

  async function saveEdit() {
    if (!editingItem) return
    const id = editingItem.id
    try {
      // Hydracker est fermé depuis le 03/09 : ces corrections passent par
      // PATCH /api/v1/{nzbs,ddls}/{id} côté Elysium.
      if (myTab === 'nzbs') {
        await UpdateMyElyNzb(id, editPayload.quality, editPayload.language, editPayload.subtitles)
      } else {
        await UpdateMyElyDdl(id, editPayload.quality, editPayload.language, editPayload.subtitles)
      }
      addLog('MY', `✓ Modifié ${myTab.slice(0,-1)} #${id}`)
      try { Notify('✓ Modifié', `${myTab.slice(0,-1)} #${id}`) } catch(e) {}
      editingItem = null
      loadMyUploads()
    } catch(e) {
      addLog('MY', `✗ ${e}`)
    }
  }

  function startDelete(item) {
    deletingItem = item
    deleteConfirmInput = ''
  }
  function cancelDelete() { deletingItem = null; deleteConfirmInput = '' }

  async function confirmDelete() {
    if (!deletingItem) return
    // Vérif : l'user doit avoir tapé l'ID exact pour confirmer
    if (deleteConfirmInput.trim() !== String(deletingItem.id)) {
      addLog('MY', '⚠ Suppression annulée : ID ne correspond pas')
      return
    }
    const id = deletingItem.id
    try {
      if (myTab === 'liens') {
        await DeleteMyElyDdl(id)
      } else if (myTab === 'nzbs') {
        await DeleteMyElyNzb(id)
      }
      addLog('MY', `✓ Supprimé ${myTab.slice(0,-1)} #${id}`)
      try { Notify('Supprimé', `${myTab.slice(0,-1)} #${id}`) } catch(e) {}
      deletingItem = null
      deleteConfirmInput = ''
      loadMyUploads()
    } catch(e) {
      addLog('MY', `✗ ${e}`)
    }
  }









// --- Log API : capture les entrées api:log émises par le client Go ---
  let apiLogs = []                  // []APILogEntry (max 500 entries, FIFO)
  let apiLogFilter = 'all'          // 'all' | 'ok' | 'error'
  let apiLogSearch = ''
  let apiLogExpanded = {}           // { [idx]: true } pour afficher le body_preview
  const API_LOG_MAX = 500

  function clearApiLogs() { apiLogs = []; apiLogExpanded = {} }

  $: filteredApiLogs = apiLogs.filter(e => {
    if (apiLogFilter === 'ok' && (e.error || e.status >= 400)) return false
    if (apiLogFilter === 'error' && !e.error && e.status < 400) return false
    if (apiLogSearch) {
      const q = apiLogSearch.toLowerCase()
      if (!e.url?.toLowerCase().includes(q) && !e.body_preview?.toLowerCase().includes(q) && !e.error?.toLowerCase().includes(q)) return false
    }
    return true
  })

  let checkState = {}  // { [hash]: { stage, msg, percent, speed } }
  // Reset page sur changement de filtre ou tri







  onMount(async () => {
    try {
      const loaded = await GetConfig()
      if (loaded) cfg = { ...cfg, ...loaded }
    } catch {}
    try { appVersion = await GetVersion() } catch {}
    try { updateInfo = await CheckForUpdate() } catch {}
    // Identité : on interroge Hydracker via son token bearer pour récupérer le
    // pseudo (sert au filtre "Mes uploads"). Si pas de token / pas de réponse,
    // myUsername reste vide → "Mes uploads" affiche tous les uploads.
    try {
      const me = await GetUserProfile('me')
      if (me && me.name) {
        myUsername = me.name
        fetchAvatar()
      }
    } catch {}
    checkLihdlPasswordStatus()
    checkSeedboxPasswordStatus()
    try { hydrackerURLManaged = await IsHydrackerURLManaged() } catch {}
    if (hydrackerURLManaged) {
      try { cfg.hydracker_base_url = await GetEffectiveHydrackerURL() } catch {}
    }
    EventsOn('watch:status', s => { watchRunning = !!s.running; if (s.running) addLog('WATCH', `Surveillance active : ${s.folder}`) })
    EventsOn('update:progress', p => { updateState = { ...updateState, stage: p.stage || '', msg: p.msg || '', percent: p.percent || 0 } })
    EventsOn('watch:newfile', path => {
      addLog('WATCH', `→ Nouveau fichier : ${path}`)
      window.dispatchEvent(new CustomEvent('watch:newfile', { detail: path }))
    })
    // Démarrage auto si configuré
    if (cfg.watch_auto_start && cfg.watch_folder) {
      try { await StartWatchFolder(cfg.watch_folder) } catch(e) { addLog('WATCH', '✗ ' + e) }
    }
    EventsOn('nzb:status', s => {
      nzbStatus = s
      if (s === 'Terminé') nzbDone = true
      addLog('NZB', s)
    })
    EventsOn('nzb:parpar', p => { if (p.percent !== undefined) nzbParparPct = p.percent })
    EventsOn('nzb:nyuu',   p => {
      if (p.percent  !== undefined) nzbNyuuPct     = p.percent
      if (p.speed    !== undefined) nzbNyuuSpeed   = p.speed
      if (p.eta      !== undefined) nzbNyuuETA     = p.eta
      if (p.articles !== undefined) nzbNyuuArticles = p.articles
    })
    EventsOn('nzb:result', r => { nzbResult = r; nzbDone = true; addLog('NZB', r.message) })
    EventsOn('ddl:log',    msg => addLog('DDL', msg))
    EventsOn('bin:log',    msg => addLog('BIN', msg))
    EventsOn('check:log',  msg => addLog('CHK', msg))
    EventsOn('check:status', p => {
      if (!p.hash) return
      checkState = { ...checkState, [p.hash]: { ...(checkState[p.hash] || {}), stage: p.stage, msg: p.msg } }
      addLog('CHK', p.msg)
    })
    EventsOn('check:progress', p => {
      if (!p.hash) return
      checkState = { ...checkState, [p.hash]: { ...(checkState[p.hash] || {}), percent: p.percent ?? 0, speed: p.speed ?? 0 } }
    })
    EventsOn('reseed:status', p => {
      reseedStage = p.stage || ''
      reseedMsg = p.msg || ''
      addLog('RES', p.msg || p.stage)
    })
    EventsOn('autoreseed:status', p => {
      autoReseedStatus = p.msg || p.stage || ''
      if (p.msg) addLog('AUTO', p.msg)
      // Route l'état vers le torrent actif de l'onglet Check Torrent
      if (checkActionActiveID) {
        checkActionProg = { ...checkActionProg, [checkActionActiveID]: {
          ...(checkActionProg[checkActionActiveID] || {}),
          stage: p.stage || 'progress',
          msg: p.msg || '',
        } }
      }
    })
    EventsOn('autoreseed:progress', p => {
      if (checkActionActiveID) {
        checkActionProg = { ...checkActionProg, [checkActionActiveID]: {
          ...(checkActionProg[checkActionActiveID] || { stage: 'progress', msg: '' }),
          percent: p.percent || 0,
          speed_mb: p.speed_mb || 0,
        } }
      }
    })
    EventsOn('autoreseed_ddl:status', p => {
      autoReseedStatus = p.msg || p.stage || ''
      if (p.msg) addLog('AUTO', p.msg)
    })
    EventsOn('autoreseed_ddl:progress', p => {
      autoReseedDDLProgress = {
        percent: p.percent || 0,
        speed_mb: p.speed_mb || 0,
        bytes: p.bytes || 0,
        total: p.total || 0,
      }
    })
    EventsOn('api:log', entry => {
      apiLogs = [entry, ...apiLogs].slice(0, API_LOG_MAX)
    })
    // Events du workflow complet (DDL→FTP+torrent+recheck) — on les route vers
    // la demande active (reqActiveID) pour afficher barre + vitesse dans le UI.
    EventsOn('autoreseed_full:status', p => {
      if (reqActiveID) {
        reqProgress = { ...reqProgress, [reqActiveID]: { ...(reqProgress[reqActiveID] || {}), stage: p.stage || '', msg: p.msg || '' } }
      }
      if (checkActionActiveID) {
        checkActionProg = { ...checkActionProg, [checkActionActiveID]: {
          ...(checkActionProg[checkActionActiveID] || {}),
          stage: p.stage || 'progress',
          msg: p.msg || '',
        } }
      }
      if (p.msg) addLog('AUTO', p.msg)
    })
    EventsOn('autoreseed_full:progress', p => {
      if (reqActiveID) {
        reqProgress = { ...reqProgress, [reqActiveID]: {
          ...(reqProgress[reqActiveID] || {}),
          stage: 'ftp',
          percent: p.percent || 0,
          speed_mb: p.speed_mb || 0,
          bytes: p.bytes || 0,
          total: p.total || 0,
        } }
      }
      if (checkActionActiveID) {
        checkActionProg = { ...checkActionProg, [checkActionActiveID]: {
          ...(checkActionProg[checkActionActiveID] || { stage: 'ftp', msg: '' }),
          percent: p.percent || 0,
          speed_mb: p.speed_mb || 0,
        } }
      }
    })
    EventsOn('autoreseed_full:seedbox', p => {
      if (reqActiveID) {
        reqProgress = { ...reqProgress, [reqActiveID]: {
          ...(reqProgress[reqActiveID] || {}),
          stage: 'seedbox',
          msg: `Seedbox : ${(p.percent || 0).toFixed(0)}%`,
        } }
      }
      if (checkActionActiveID) {
        checkActionProg = { ...checkActionProg, [checkActionActiveID]: {
          ...(checkActionProg[checkActionActiveID] || {}),
          stage: 'seedbox',
          percent: p.percent || 0,
          speed_mb: p.speed_mb || 0,
          msg: `Seedbox : ${(p.percent || 0).toFixed(0)}%`,
        } }
      }
    })
    EventsOn('reseed:progress', p => {
      reseedPct = p.percent ?? 0
      reseedSpeed = p.speed_mb ?? 0
    })
  })

  onDestroy(() => EventsOff('nzb:status', 'nzb:parpar', 'nzb:nyuu', 'nzb:result', 'ddl:log'))

  async function saveConfig() {
    await SaveConfig(cfg)
    cfgSaved = true
    setTimeout(() => cfgSaved = false, 2000)
  }

  // --- Tests ---
  let testResults = {}
  let testLoading = {}

  async function runTest(key, fn) {
    testLoading[key] = true
    testLoading = testLoading
    try {
      testResults[key] = await fn()
    } catch(e) {
      testResults[key] = { ok: false, message: e.toString() }
    }
    testLoading[key] = false
    testLoading = testLoading
    testResults = testResults
  }
</script>

<div class="layout">
  <!-- Sidebar -->
  <aside class="sidebar" class:collapsed={sidebarCollapsed}>
    <button class="sidebar-toggle" on:click={() => sidebarCollapsed = !sidebarCollapsed} title={sidebarCollapsed ? 'Déplier la barre' : 'Replier la barre'}>
      {sidebarCollapsed ? '›' : '‹'}
    </button>
    <div class="brand">
      <!-- Pivot 2026-09-03 : Hydracker fermé, Elysium devient la cible unique. -->
      <!-- Losange en CSS pur. Le mot ELYSIUM est du texte HTML en Oxanium,
           donc il suit la typographie de l'interface au lieu d'être gravé
           dans un SVG avec une police système. -->
      <div class="mark" aria-hidden="true"></div>
      {#if !sidebarCollapsed}
        <div class="wordmark">ELYSIUM</div>
        <div class="logo">GO Post Tools</div>
        {#if appVersion}<div class="brand-version">v{appVersion}</div>{/if}
        <div class="brand-author">By ASTROBOY</div>
      {/if}
    </div>

    <!-- Carte user retirée : plus d'auth (identité via token Hydracker) -->
    {#if myUsername && !sidebarCollapsed}
      <div style="text-align:center;font-size:11px;color:var(--text3);margin-bottom:8px">
        Connecté : <b style="color:var(--text2)">{myUsername}</b>
      </div>
    {/if}

    <nav>
      {#each visibleTabs as tab}
        <button class="nav-item" class:active={activeTab === tab.id}
          on:click={() => activeTab = tab.id} title={sidebarCollapsed ? tab.label : ''}>
          <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"
               stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d={tab.icon}/>
          </svg>
          {#if !sidebarCollapsed}<span class="nav-text">{tab.label}</span>{/if}
        </button>
      {/each}
    </nav>
    <div class="sidebar-footer">
      {#if updateInfo?.available}
        <button class="btn-update" on:click={() => showUpdateModal = true} title="Télécharger la nouvelle version">
          {@html ICO_SPARK}{#if !sidebarCollapsed}<span>Mise à jour v{updateInfo.latest}</span>{/if}
        </button>
      {:else}
        <button class="btn-check-update" on:click={recheckUpdate} disabled={updateChecking} title="Vérifier les mises à jour">
          <span class:up-spinning={updateChecking}>{@html ICO_REFRESH}</span>
          {#if !sidebarCollapsed}<span>{updateCheckMsg || 'Vérifier maj'}</span>{/if}
        </button>
      {/if}
      <!-- Bouton logout retiré : plus d'auth -->
    </div>
  </aside>

  <!-- Main content -->
  <main class="content">

    <!-- HydrackerTab toujours monté pour préserver l'état -->
    <div style="display:{activeTab === 'hydracker' ? 'contents' : 'none'}">
      <HydrackerTab />
    </div>

    {#if activeTab !== 'hydracker'}

    <!-- ===== FICHES ===== -->
    {#if activeTab === 'myuploads'}
      <div class="tab-content">
        <h2>{@html ICO_ARCHIVE}Mes uploads</h2>

        <div class="section">
          <div class="section-header">
            <span>Source</span>
            <span class="up-user">compte <b class="mono">{myUsername || '—'}</b></span>
          </div>
          <div class="chip-row">
            <button class="btn-test" class:active-chip={myTab === 'liens'} on:click={() => { myTab = 'liens'; myPage = 1; loadMyUploads() }}>{@html ICO_LINK}DDL</button>
            <button class="btn-test" class:active-chip={myTab === 'nzbs'} on:click={() => { myTab = 'nzbs'; myPage = 1; loadMyUploads() }}>{@html ICO_NEWS}NZB</button>
            <button class="btn-test up-refresh" class:up-spinning={myLoading} on:click={loadMyUploads} disabled={myLoading}>{@html ICO_REFRESH}Rafraîchir</button>
          </div>
        </div>

        <div class="section">
          <div class="section-header">
            <span>{myTab === 'liens' ? 'Liens DDL' : 'NZB'}</span>
            <span class="up-count"><b class="mono">{myItems.length}</b> affiché{myItems.length > 1 ? 's' : ''}</span>
            {#if myTotalPages > 1}
              <div class="up-pager">
                <button class="btn-test" on:click={() => { if (myPage > 1) { myPage--; loadMyUploads() } }} disabled={myPage <= 1}>‹</button>
                <span class="mono">{myPage} / {myTotalPages}</span>
                <button class="btn-test" on:click={() => { if (myPage < myTotalPages) { myPage++; loadMyUploads() } }} disabled={myPage >= myTotalPages}>›</button>
              </div>
            {/if}
          </div>

          {#if myLoading && myItems.length === 0}
            <div class="up-state">Chargement…</div>
          {:else if myError}
            <div class="up-state up-state-err">{@html ICO_WARN}{myError}</div>
          {:else if myItems.length === 0}
            <div class="up-state">Aucun item sur cette page.</div>
          {:else}
            {#each myItems as item, i}
              <div class="up-row" style="animation-delay:{Math.min(i, 14) * 28}ms">
                <div class="up-main">
                  <div class="up-title">
                    <span class="up-id mono">#{item.id}</span>
                    <span class="up-name">{item.name || item.torrent_name || (myTab === 'liens' ? item.lien : '(sans nom)')}</span>
                  </div>
                  <div class="up-meta">
                    <span class="up-chip">fiche <b class="mono">#{item.title_id}</b></span>
                    {#if item.quality}<span class="up-chip">{item.quality}</span>{/if}
                    {#if item.saison || item.episode}<span class="up-chip mono">S{String(item.saison||0).padStart(2,'0')}E{String(item.episode||0).padStart(2,'0')}</span>{/if}
                    {#if item.taille || item.size}<span class="up-chip mono">{((item.taille||item.size)/1e9).toFixed(2)} Go</span>{/if}
                    {#if item.host?.name}<span class="up-chip">{item.host.name}</span>{/if}
                    <span class="up-chip">{@html ICO_CAL}<span class="mono">{(item.created_at || '').slice(0,10)}</span></span>
                    {#if item.active === 0}<span class="up-chip up-chip-warn">{@html ICO_WARN}inactif</span>{/if}
                  </div>
                </div>
                <div class="up-actions">
                  <button class="btn-test" on:click={() => startEdit(item)} title="Modifier">{@html ICO_PEN}Modifier</button>
                  <button class="btn-test btn-danger" on:click={() => startDelete(item)} title="Supprimer">{@html ICO_TRASH}Supprimer</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        <!-- Modal Édition -->
        {#if editingItem}
          <div class="modal-backdrop" on:click|self={cancelEdit}>
            <div class="modal-card">
              <div class="modal-title">{@html ICO_PEN}Modifier #{editingItem.id}</div>
              <div class="modal-hint">{editingItem.name || editingItem.file_name || ''}</div>
              {#if !elyMeta}
                <div class="up-state">Chargement des listes Elysium…</div>
              {:else}
                <div class="field">
                  <label for="edit-quality">Qualité</label>
                  <select id="edit-quality" bind:value={editPayload.quality}>
                    <option value="">— Aucune —</option>
                    {#each Object.entries(elyMeta.qualities || {}) as [cle, libelle]}
                      <option value={cle}>{libelle}</option>
                    {/each}
                  </select>
                </div>
                <div class="field">
                  <label>Langues</label>
                  <div class="chip-row">
                    {#each Object.entries(elyMeta.languages || {}) as [cle, libelle]}
                      <button class="btn-test" class:active-chip={editPayload.language.includes(cle)}
                              on:click={() => editPayload.language = basculer(editPayload.language, cle)}>{libelle}</button>
                    {/each}
                  </div>
                </div>
                <div class="field">
                  <label>Sous-titres</label>
                  <div class="chip-row">
                    {#each Object.entries(elyMeta.subtitles || {}) as [cle, libelle]}
                      <button class="btn-test" class:active-chip={editPayload.subtitles.includes(cle)}
                              on:click={() => editPayload.subtitles = basculer(editPayload.subtitles, cle)}>{libelle}</button>
                    {/each}
                  </div>
                </div>
              {/if}
              <div class="post-actions">
                <button class="btn-save" on:click={saveEdit}>{@html ICO_SAVE}Enregistrer</button>
                <button class="btn-test" on:click={cancelEdit}>Annuler</button>
              </div>
            </div>
          </div>
        {/if}

        <!-- Modal Suppression -->
        {#if deletingItem}
          <div class="modal-backdrop" on:click|self={cancelDelete}>
            <div class="modal-card modal-card-danger">
              <div class="modal-title modal-title-danger">{@html ICO_TRASH}Suppression définitive</div>
              <div class="modal-hint">Tu es sur le point de supprimer <b>définitivement</b> cet item :</div>
              <div class="up-del-box">
                <div><b class="mono">#{deletingItem.id}</b> · {deletingItem.name || deletingItem.torrent_name || deletingItem.lien || ''}</div>
                <div class="up-del-sub">fiche <span class="mono">#{deletingItem.title_id}</span>{deletingItem.saison || deletingItem.episode ? ` · S${String(deletingItem.saison||0).padStart(2,'0')}E${String(deletingItem.episode||0).padStart(2,'0')}` : ''}</div>
              </div>
              <div class="field">
                <label>Tape l'ID <b class="mono">{deletingItem.id}</b> pour confirmer :</label>
                <input type="text" bind:value={deleteConfirmInput} placeholder={deletingItem.id} />
              </div>
              <div class="post-actions">
                <button class="btn-save btn-save-danger" on:click={confirmDelete} disabled={deleteConfirmInput.trim() !== String(deletingItem.id)}>
                  {@html ICO_TRASH}Supprimer définitivement
                </button>
                <button class="btn-test" on:click={cancelDelete}>Annuler</button>
              </div>
            </div>
          </div>
        {/if}
      </div>

    <!-- ===== LOGS : Requêtes API (sous-onglet) ===== -->
    {:else if activeTab === 'logs' && logsSubTab === 'api'}
      <div class="tab-content">
        <h2>{@html ICO_TERMINAL}Logs</h2>
        <div class="sub-tabs-nav">
          <button class:active={logsSubTab === 'journal'} on:click={() => logsSubTab = 'journal'}>{@html ICO_LIST}Journal app</button>
          <button class:active={logsSubTab === 'api'} on:click={() => logsSubTab = 'api'}>{@html ICO_TERMINAL}Requêtes API</button>
        </div>

        <div class="section">
          <div class="section-header">
            <span>Requêtes capturées</span>
            <span class="log-count"><b class="mono">{filteredApiLogs.length}</b> / <span class="mono">{apiLogs.length}</span></span>
            <button class="btn-test btn-danger" on:click={clearApiLogs}>{@html ICO_TRASH}Vider</button>
          </div>
          <div class="chip-row api-filters">
            <button class="btn-test" class:active-chip={apiLogFilter === 'all'} on:click={() => apiLogFilter = 'all'}>Toutes</button>
            <button class="btn-test" class:active-chip={apiLogFilter === 'ok'} on:click={() => apiLogFilter = 'ok'}>{@html ICO_CHECK}OK (2xx)</button>
            <button class="btn-test" class:active-chip={apiLogFilter === 'error'} on:click={() => apiLogFilter = 'error'}>{@html ICO_CROSS}Erreurs</button>
          </div>
          <div class="log-search api-search">
            {@html ICO_SEARCH}
            <input type="text" bind:value={apiLogSearch} placeholder="Filtrer par URL, corps de réponse ou erreur…" />
            {#if apiLogSearch}<button class="log-search-x" title="Effacer" on:click={() => apiLogSearch = ''}>✕</button>{/if}
          </div>
        </div>

        <div class="section">
          {#if filteredApiLogs.length === 0}
            <div class="up-state">Aucune requête — fais une action dans l'app pour en générer.</div>
          {:else}
            {#each filteredApiLogs as e, i}
              <div class="api-log-row" class:api-log-err={e.error || e.status >= 400}
                   on:click={() => apiLogExpanded = { ...apiLogExpanded, [i]: !apiLogExpanded[i] }}>
                <div class="api-log-head">
                  <span class="api-log-ts mono">{e.ts}</span>
                  <span class="api-log-method api-log-method-{e.method}">{e.method}</span>
                  <span class="api-log-status mono"
                        class:api-st-ok={e.status >= 200 && e.status < 300}
                        class:api-st-err={e.status >= 400}>{e.status || '—'}</span>
                  <span class="api-log-dur mono">{e.duration_ms} ms</span>
                  <span class="api-log-url mono">{e.url}</span>
                </div>
                {#if e.error}
                  <div class="api-log-error">{@html ICO_WARN}{e.error}</div>
                {/if}
                {#if apiLogExpanded[i] && e.body_preview}
                  <pre class="api-log-body">{e.body_preview}</pre>
                {/if}
              </div>
            {/each}
          {/if}
        </div>
      </div>

    <!-- ===== HISTORIQUE ===== -->
    {:else if activeTab === 'history'}
      <div class="tab-content">
        <h2>{@html ICO_HISTORY}Historique</h2>
        <div class="hist-stats">
          <span class="hist-stat">Total <b>{(histStats.nzb || 0) + (histStats.ddl || 0)}</b></span>
          <span class="hist-stat ok">OK <b>{histStats.ok}</b></span>
          <span class="hist-stat err">Erreurs <b>{histStats.error}</b></span>
          <span class="hist-stat">NZB <b>{histStats.nzb}</b></span>
          <span class="hist-stat">DDL <b>{histStats.ddl}</b></span>
        </div>
        <div class="hist-filters">
          <div class="hist-search-wrap">
            {@html ICO_SEARCH}
            <input class="hist-search" type="text" placeholder="Recherche (titre, fichier, lien…)" bind:value={histQuery} on:input={loadHistory} />
          </div>
          <div class="hist-type-btns">
            <button class:active={histFilter === ''} on:click={() => { histFilter = ''; loadHistory() }}>Tous</button>
            <button class:active={histFilter === 'nzb'} on:click={() => { histFilter = 'nzb'; loadHistory() }}>{@html ICO_NEWS}NZB</button>
            <button class:active={histFilter === 'ddl'} on:click={() => { histFilter = 'ddl'; loadHistory() }}>{@html ICO_LINK}DDL</button>
          </div>
        </div>
        {#if histLoading}
          <div class="hist-empty">Chargement…</div>
        {:else if histEntries.length === 0}
          <div class="hist-empty">Aucune entrée. Les posts s'ajoutent automatiquement ici.</div>
        {:else}
          <div class="hist-list">
            {#each histEntries.filter(e => e.type !== 'torrent') as e}
              <div class="hist-row" class:err={e.status === 'error'}>
                <div class="hist-col-date">{formatDate(e.timestamp)}</div>
                <div class="hist-col-type hist-type-{e.type}">{e.type.toUpperCase()}</div>
                <div class="hist-col-main">
                  <div class="hist-title">{e.title_name || `#${e.title_id}`}{#if e.saison || e.episode} · S{String(e.saison).padStart(2,'0')}E{String(e.episode).padStart(2,'0')}{/if}</div>
                  <div class="hist-sub">{e.qualite_name || ''} · {e.filename}</div>
                  {#if e.links}<div class="hist-links">{e.links}</div>{/if}
                  {#if e.error}<div class="hist-error">{@html ICO_CROSS}{e.error}</div>{/if}
                </div>
                <div class="hist-col-id">#{e.hydracker_id || '—'}</div>
                <button class="hist-del" on:click={() => deleteHistEntry(e.id)} title="Supprimer">✕</button>
              </div>
            {/each}
          </div>
        {/if}
      </div>

    <!-- ===== RESEED : Depuis URL (sous-onglet) ===== -->
    {:else if activeTab === 'settings'}
      <div class="tab-content">
        <h2>{@html ICO_COG}Réglages</h2>
        <div class="sections">

          <!-- ===== Clés API ===== -->
          <div class="settings-group">{@html ICO_KEY}Clés API</div>

          <div class="section section-locked">
            <div class="section-header">
              <span>{@html ICO_LOCK}TMDB (verrouillé team)</span>
              <button class="btn-test" on:click={() => runTest('tmdb', () => TestTMDB(''))}>
                {#if testLoading.tmdb}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.tmdb}
              <div class="test-result" class:ok={testResults.tmdb.ok}>{testResults.tmdb.message}</div>
            {/if}
            <div class="settings-hint">
              <span class="settings-hint-ok">{@html ICO_CHECK}Recherche TMDB via le proxy configuré — pas de clé requise.</span><br>
              Bonus : notes IMDb fusionnées + lookup par IMDb ID + watch providers (Netflix/Disney+…).
            </div>
            <div class="field">
              <label>Index de recherche TMDB (proxy URL)</label>
              <input type="password" value={cfg.tmdb_proxy_url} disabled readonly />
              <div class="field-hint">URL imposée par la team — non modifiable.</div>
            </div>
            <div class="field">
              <label>Index de recherche LiHDL (par nom de fichier)</label>
              <input type="password" value={cfg.media_search_url} disabled readonly />
              <div class="field-hint">Endpoint custom imposé par la team — non modifiable.</div>
            </div>
            <div class="field">
              <label>Index de recherche TEAM</label>
              <input type="password" value={cfg.lihdl_base_url} disabled readonly />
              <div class="field-hint">Dossier LiHDL team-shared — verrouillé par la team. Clé perso TMDB → carte "TMDB" plus bas.</div>
            </div>
          </div>

          <!-- Cartes éditables : clés perso utilisateur (TMDB + Hydracker) -->
          <div class="section">
            <div class="section-header">
              <span>TMDB</span>
              <button class="btn-test" on:click={() => runTest('tmdb', () => TestTMDB(cfg.tmdb_api_key))}>
                {#if testLoading.tmdb}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.tmdb}
              <div class="test-result" class:ok={testResults.tmdb.ok}>{testResults.tmdb.message}</div>
            {/if}
            <div class="field">
              <label>Clé API TMDB</label>
              <input type="password" bind:value={cfg.tmdb_api_key} placeholder="API key" />
              <div class="field-hint">Fallback si le proxy team est down. Récupère ta clé sur themoviedb.org → Settings → API.</div>
            </div>
          </div>

          <div class="section">
            <div class="section-header">
              <span>1Fichier</span>
              <button class="btn-test" on:click={() => runTest('onefichier', () => TestOneFichier(((cfg.one_fichier_accounts || []).find(c => c.api_key) || {}).api_key || cfg.one_fichier_api_key))}>
                {#if testLoading.onefichier}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.onefichier}
              <div class="test-result" class:ok={testResults.onefichier.ok}>{testResults.onefichier.message}</div>
            {/if}
            <div class="field-hint" style="margin-bottom:10px">
              Un compte par ligne. Celui qui sert à poster se choisit à côté de la pastille DDL,
              sur l'écran Publier, sans repasser par ici.
            </div>
            {#each cfg.one_fichier_accounts || [] as c, i}
              <div class="c1f-row">
                <input class="c1f-label" type="text" bind:value={c.label} placeholder="Nom (ex : GANDALF)" />
                <input class="c1f-key" type="password" bind:value={c.api_key} placeholder="Clé API" />
                <input class="c1f-folder" type="text" bind:value={c.folder} placeholder="Dossier (optionnel)" />
                <button class="btn-test btn-danger c1f-del" title="Retirer ce compte"
                        on:click={() => cfg.one_fichier_accounts = (cfg.one_fichier_accounts || []).filter((_, j) => j !== i)}>{@html ICO_TRASH}</button>
              </div>
              {#if c.api_key && !cleApparenteValide(c.api_key)}
                <div class="c1f-alerte">
                  Ceci ne ressemble pas à une clé API ({c.api_key.trim().length} caractères).
                  Une clé 1Fichier fait une trentaine de caractères, lettres et chiffres uniquement —
                  à récupérer sur 1fichier.com, Mon compte → Paramètres → API.
                </div>
              {/if}
            {/each}
            <div class="chip-row" style="margin-top:8px">
              <button class="btn-test" on:click={() => cfg.one_fichier_accounts = [...(cfg.one_fichier_accounts || []), { label: '', api_key: '', folder: 'GO POST TOOLS' }]}>
                + Ajouter un compte
              </button>
            </div>
            <div class="field-hint" style="margin-top:10px">
              Le dossier de rangement est propre à chaque compte : après l'upload, le fichier y est
              déplacé (créé si absent), ou laissé à la racine si le champ est vide. La gestion des
              dossiers par l'API demande un tier 1F Premium ou Gold.
              <br>⚠ Une clé 1Fichier est liée à <b>une seule adresse IP</b> : plusieurs comptes depuis
              cette machine ne posent aucun problème, mais la même clé utilisée ailleurs sera refusée.
            </div>
          </div>

          <div class="section">
            <div class="section-header">
              <span>IGDB (Jeux vidéo)</span>
              <button class="btn-test" on:click={() => runTest('igdb', async () => ({ ok: true, message: await TestIgdb(cfg.igdb_client_id, cfg.igdb_client_secret) }))}>
                {#if testLoading.igdb}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.igdb}
              <div class="test-result" class:ok={testResults.igdb.ok}>{testResults.igdb.message}</div>
            {/if}
            <div class="field">
              <label>Twitch Client ID</label>
              <input type="password" bind:value={cfg.igdb_client_id} placeholder="client id" />
            </div>
            <div class="field">
              <label>Twitch Client Secret</label>
              <input type="password" bind:value={cfg.igdb_client_secret} placeholder="client secret" />
              <div class="field-hint">Active la recherche de jeux vidéo (toggle « Jeu » dans l'onglet Publier). Crée une app sur console.twitch.tv → Applications → Enregistrer (gratuit), puis colle le Client ID + Secret.</div>
            </div>
          </div>

          <div class="section">
            <div class="section-header">
              <span>Elysium (cross-post)</span>
              <button class="btn-test" on:click={() => runTest('elysium', async () => ({ ok: true, message: await TestElysium(cfg.elysium_api_token) }))}>
                {#if testLoading.elysium}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.elysium}
              <div class="test-result" class:ok={testResults.elysium.ok}>{testResults.elysium.message}</div>
            {/if}
            <div class="field">
              <label>Token API Elysium</label>
              <input type="password" bind:value={cfg.elysium_api_token} placeholder="els_..." />
              <div class="field-hint">Cible unique des posts depuis le 2026-09-03 (Hydracker fermé). Génère ton token sur <a href="https://elysium-les5zamis.com/account-settings" target="_blank">elysium-les5zamis.com/account-settings</a> section API tokens.</div>
            </div>
          </div>

          <!-- ===== Configuration ===== -->
          <div class="settings-group settings-group-spaced">{@html ICO_ARCHIVE}Outils embarqués</div>
          <div class="section">
            <div class="section-header">
              <span>parpar · nyuu · mediainfo</span>
              <span class="up-count">compilés dans l'app</span>
            </div>
            {#if !outils.length}
              <div class="up-state">Lecture des outils…</div>
            {:else}
              {#each outils as o}
                <div class="outil" class:outil-warn={o.source !== 'embarqué'} class:outil-err={!o.path}>
                  <span class="outil-nom mono">{o.name}</span>
                  <span class="outil-src">{o.path ? o.source : 'introuvable'}</span>
                  <span class="outil-ver mono">{o.version || '—'}</span>
                </div>
                {#if o.source !== 'embarqué' || !o.path}
                  <div class="outil-detail">{o.error || o.path}</div>
                {/if}
              {/each}
              <div class="field-hint">
                Ces trois outils sont inclus dans l'application et extraits au premier lancement :
                aucune installation n'est nécessaire. Une ligne en ambre signalerait un repli sur
                un binaire du système, ce qui ne doit pas arriver.
              </div>
            {/if}
          </div>

          <div class="settings-group settings-group-spaced">{@html ICO_COG}Configuration</div>

          <!-- Usenet -->
          <div class="section">
            <div class="section-header">
              <span>Usenet</span>
              <button class="btn-test" on:click={() => runTest('usenet', () => TestUsenet(cfg.usenet_host, cfg.usenet_port))}>
                {#if testLoading.usenet}…{:else}Tester{/if}
              </button>
            </div>
            {#if testResults.usenet}
              <div class="test-result" class:ok={testResults.usenet.ok}>{testResults.usenet.message}</div>
            {/if}
            <div class="fields-grid">
              <div class="field">
                <label>Serveur</label>
                <input type="text" bind:value={cfg.usenet_host} placeholder="news.example.com" />
              </div>
              <div class="field">
                <label>Port</label>
                <input type="number" bind:value={cfg.usenet_port} />
              </div>
              <div class="field">
                <label>Utilisateur</label>
                <input type="text" bind:value={cfg.usenet_user} />
              </div>
              <div class="field">
                <label>Mot de passe</label>
                <input type="password" bind:value={cfg.usenet_password} />
              </div>
              <div class="field">
                <label>Connexions</label>
                <input type="number" bind:value={cfg.usenet_connections} />
              </div>
              <div class="field field-checkbox">
                <label>
                  <input type="checkbox" bind:checked={cfg.usenet_ssl} />
                  SSL/TLS
                </label>
              </div>
              <div class="field">
                <label for="usenet-group">Newsgroup</label>
                <input id="usenet-group" type="text" bind:value={cfg.usenet_group} placeholder="alt.binaries.test" />
              </div>
            </div>
          </div>

          <!-- Watch folder -->
          <div class="section">
            <div class="section-header">
              <span>Dossier surveillé (auto-post)</span>
              {#if watchRunning}
                <button class="btn-test btn-danger" on:click={async () => { try { await StopWatchFolder() } catch(e){} }}>{@html ICO_STOP}Arrêter</button>
              {:else}
                <button class="btn-test" on:click={async () => { try { await StartWatchFolder(cfg.watch_folder) } catch(e){ addLog('WATCH', '✗ ' + e) } }}>{@html ICO_PLAY}Démarrer</button>
              {/if}
            </div>
            <div class="field">
              <label for="watch-folder">Chemin</label>
              <input id="watch-folder" type="text" bind:value={cfg.watch_folder} placeholder="/Users/gandalf/Desktop/LiHDL" />
              <div class="field-hint">Chaque nouveau .mkv/.mp4 dans ce dossier déclenche l'analyse (et l'auto-post si Full Auto est activé).</div>
            </div>
            <div class="field field-checkbox">
              <label>
                <input type="checkbox" bind:checked={cfg.watch_auto_start} />
                Démarrer automatiquement au lancement de l'app
              </label>
            </div>
            {#if watchRunning}
              <div class="test-result ok">● Surveillance active sur : {cfg.watch_folder}</div>
            {/if}
          </div>



          <!-- ParPar -->
          <div class="section">
            <div class="section-header"><span>ParPar (PAR2)</span></div>
            <div class="fields-grid">
              <div class="field">
                <label for="parpar-redundancy">Redondance (%)</label>
                <input id="parpar-redundancy" type="number" bind:value={cfg.parpar_redundancy} min="1" max="100" step="1" />
              </div>
              <div class="field">
                <label for="parpar-threads">Threads</label>
                <input id="parpar-threads" type="number" bind:value={cfg.parpar_threads} min="1" max="64" />
              </div>
              <div class="field">
                <label for="parpar-slice">Slice size (octets)</label>
                <input id="parpar-slice" type="number" bind:value={cfg.parpar_slice_size} step="1024" />
              </div>
            </div>
          </div>


        </div>

        <button class="btn-save" on:click={saveConfig}>
          {@html cfgSaved ? ICO_CHECK : ICO_SAVE}{cfgSaved ? 'Sauvegardé' : 'Sauvegarder'}
        </button>
      </div>

    <!-- ===== LOGS : Journal app (sous-onglet) ===== -->
    {:else if activeTab === 'logs' && logsSubTab === 'journal'}
      <div class="tab-content">
        <h2>{@html ICO_TERMINAL}Logs</h2>
        <div class="sub-tabs-nav">
          <button class:active={logsSubTab === 'journal'} on:click={() => logsSubTab = 'journal'}>{@html ICO_LIST}Journal app</button>
          <button class:active={logsSubTab === 'api'} on:click={() => logsSubTab = 'api'}>{@html ICO_TERMINAL}Requêtes API</button>
        </div>
        {#if $logEntries.length === 0}
          <p class="coming-soon">Les logs en temps réel apparaîtront ici</p>
        {:else}
          <div class="section">
            <div class="section-header">
              <span>Filtres</span>
              <span class="log-count"><b class="mono">{logsShown.length}</b> / <span class="mono">{$logEntries.length}</span> lignes</span>
            </div>

            <div class="log-search">
              {@html ICO_SEARCH}
              <input type="text" bind:value={logQuery} placeholder="Rechercher — ex. 482, nyuu, TMDB, échec…" />
              {#if logQuery}<button class="log-search-x" title="Effacer la recherche" on:click={() => logQuery = ''}>✕</button>{/if}
            </div>

            {#if logTags.length > 1}
              <div class="log-tags">
                {#each logTags as t}
                  <button class="log-tag log-prefix-{t.toLowerCase()}"
                          class:log-tag-off={logTagsOff.includes(t)}
                          title={logTagsOff.includes(t) ? 'Afficher ' + t : 'Masquer ' + t}
                          on:click={() => toggleLogTag(t)}>{t}</button>
                {/each}
                {#if logTagsOff.length}
                  <button class="btn-test log-tag-reset" on:click={() => logTagsOff = []}>Tout afficher</button>
                {/if}
              </div>
            {/if}

            <div class="log-actions">
              <button class="btn-test" on:click={copyLogs} title="Copier les lignes affichées">
                {@html logCopied ? ICO_CHECK : ICO_COPY}{logCopied ? 'Copié' : 'Copier'}
              </button>
              <button class="btn-test btn-danger" on:click={clearLogs}>{@html ICO_TRASH}Effacer</button>
            </div>
          </div>

          {#if logsShown.length === 0}
            <div class="up-state">Aucune ligne ne correspond au filtre.</div>
          {:else}
            <div class="log-list">
              {#each logsShown as e}
                <div class="log-line"
                     class:log-line-err={logSeverity(e.msg) === 'err'}
                     class:log-line-warn={logSeverity(e.msg) === 'warn'}>
                  <span class="log-ts">{e.ts}</span>
                  <span class="log-prefix log-prefix-{e.prefix.toLowerCase()}">{e.prefix}</span>
                  <span class="log-msg">{e.msg}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    {/if}

    {/if}<!-- fin activeTab !== hydracker -->

  </main>
</div>

<!-- ===== Modale mot de passe LiHDL ===== -->
{#if lihdlModal}
  <div class="modal-backdrop" on:click|self={() => lihdlModal = null}>
    <div class="modal-card">
      <div class="modal-header">
        <h3>
          {#if lihdlModal === 'unlock'}{@html ICO_UNLOCK}Déverrouiller LiHDL
          {:else if lihdlModal === 'create'}{@html ICO_LOCK}Définir un mot de passe
          {:else if lihdlModal === 'change'}{@html ICO_KEY}Changer le mot de passe
          {:else}{@html ICO_TRASH}Retirer la protection{/if}
        </h3>
        <button class="modal-close" on:click={() => lihdlModal = null}>✕</button>
      </div>
      <div class="modal-body">
        <form on:submit|preventDefault={submitLihdlPwd}>
          {#if lihdlModal === 'unlock'}
            <div class="field"><label>Mot de passe</label>
              <input type="password" bind:value={lihdlPwdInput} autofocus autocomplete="off" /></div>
          {:else if lihdlModal === 'create'}
            <div class="field"><label>Nouveau mot de passe</label>
              <input type="password" bind:value={lihdlPwdNew} autofocus autocomplete="off" /></div>
            <div class="field"><label>Confirmer</label>
              <input type="password" bind:value={lihdlPwdConfirm} autocomplete="off" /></div>
          {:else if lihdlModal === 'change'}
            <div class="field"><label>Mot de passe actuel</label>
              <input type="password" bind:value={lihdlPwdCurrent} autofocus autocomplete="off" /></div>
            <div class="field"><label>Nouveau mot de passe</label>
              <input type="password" bind:value={lihdlPwdNew} autocomplete="off" /></div>
            <div class="field"><label>Confirmer</label>
              <input type="password" bind:value={lihdlPwdConfirm} autocomplete="off" /></div>
          {:else}
            <div class="field"><label>Mot de passe actuel</label>
              <input type="password" bind:value={lihdlPwdCurrent} autofocus autocomplete="off" /></div>
            <p class="modal-hint">La section redeviendra accessible sans mot de passe.</p>
          {/if}
          {#if lihdlPwdError}<p class="modal-err">✗ {lihdlPwdError}</p>{/if}
          <div class="modal-actions" style="margin-top:12px">
            <button type="button" class="btn-secondary" on:click={() => lihdlModal = null}>Annuler</button>
            <button type="submit" class="btn-primary">Valider</button>
          </div>
        </form>
      </div>
    </div>
  </div>
{/if}

<!-- ===== Modale mot de passe SEEDBOX (FTP+seedbox sections) ===== -->
{#if seedboxModal}
  <div class="modal-backdrop" on:click|self={() => seedboxModal = null}>
    <div class="modal-card">
      <div class="modal-header">
        <h3>
          {#if seedboxModal === 'unlock'}{@html ICO_UNLOCK}Déverrouiller Seedbox/FTP
          {:else if seedboxModal === 'create'}{@html ICO_LOCK}Définir un mot de passe
          {:else if seedboxModal === 'change'}{@html ICO_KEY}Changer le mot de passe
          {:else}{@html ICO_TRASH}Retirer la protection{/if}
        </h3>
        <button class="modal-close" on:click={() => seedboxModal = null}>✕</button>
      </div>
      <div class="modal-body">
        <form on:submit|preventDefault={submitSeedboxPwd}>
          {#if seedboxModal === 'unlock'}
            <div class="field"><label>Mot de passe</label>
              <input type="password" bind:value={seedboxPwdInput} autofocus autocomplete="off" /></div>
          {:else if seedboxModal === 'create'}
            <p class="modal-hint">Mot de passe partagé entre les admins. Donne-le aux 3 autres pour qu'ils puissent déverrouiller leurs sections aussi.</p>
            <div class="field"><label>Nouveau mot de passe</label>
              <input type="password" bind:value={seedboxPwdNew} autofocus autocomplete="off" /></div>
            <div class="field"><label>Confirmer</label>
              <input type="password" bind:value={seedboxPwdConfirm} autocomplete="off" /></div>
          {:else if seedboxModal === 'change'}
            <div class="field"><label>Mot de passe actuel</label>
              <input type="password" bind:value={seedboxPwdCurrent} autofocus autocomplete="off" /></div>
            <div class="field"><label>Nouveau mot de passe</label>
              <input type="password" bind:value={seedboxPwdNew} autocomplete="off" /></div>
            <div class="field"><label>Confirmer</label>
              <input type="password" bind:value={seedboxPwdConfirm} autocomplete="off" /></div>
          {:else}
            <div class="field"><label>Mot de passe actuel</label>
              <input type="password" bind:value={seedboxPwdCurrent} autofocus autocomplete="off" /></div>
            <p class="modal-hint">Les sections redeviendront accessibles sans mot de passe.</p>
          {/if}
          {#if seedboxPwdError}<p class="modal-err">✗ {seedboxPwdError}</p>{/if}
          <div class="modal-actions" style="margin-top:12px">
            <button type="button" class="btn-secondary" on:click={() => seedboxModal = null}>Annuler</button>
            <button type="submit" class="btn-primary">Valider</button>
          </div>
        </form>
      </div>
    </div>
  </div>
{/if}

<!-- ===== Modale mise à jour ===== -->
{#if showUpdateModal && updateInfo?.available}
  <div class="modal-backdrop" on:click|self={() => { if (!updateState.downloading) showUpdateModal = false }}>
    <div class="modal-card">
      <div class="modal-header">
        <h3>{@html ICO_SPARK}Mise à jour disponible</h3>
        <button class="modal-close" on:click={() => { if (!updateState.downloading) showUpdateModal = false }}>✕</button>
      </div>
      <div class="modal-body">
        <p>Version actuelle : <b>v{updateInfo.current}</b> → nouvelle : <b>v{updateInfo.latest}</b></p>
        {#if !updateState.stage}
          <p class="modal-hint">Télécharger la nouvelle version dans ton dossier Téléchargements.</p>
        {:else if updateState.downloading}
          <div class="update-progress">
            <div class="update-bar"><div class="update-bar-fill" style="width:{updateState.percent}%"></div></div>
            <div class="update-msg">{updateState.msg} · {updateState.percent.toFixed(0)}%</div>
          </div>
        {:else if updateState.stage === 'done'}
          <p class="modal-ok">✓ Téléchargé. Le Finder s'est ouvert sur le fichier. Dézippe et remplace l'app.</p>
          <p class="modal-hint" style="margin-top:8px">Chemin : <code>{updateState.downloadedPath}</code></p>
        {:else if updateState.stage === 'error'}
          <p class="modal-err">✗ Erreur : {updateState.msg}</p>
        {/if}
      </div>
      <div class="modal-actions">
        {#if !updateState.stage || updateState.stage === 'error'}
          <button class="btn-primary" on:click={startDownloadUpdate}>{@html ICO_DOWNLOAD}Télécharger</button>
          <button class="btn-secondary" on:click={() => OpenBrowser(updateInfo.url)}>Voir sur GitHub</button>
        {:else if updateState.stage === 'done'}
          <button class="btn-primary" on:click={() => showUpdateModal = false}>Fermer</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if nfoModal}
  <div class="nfo-modal-bg" on:click={closeNfo}>
    <div class="nfo-modal-card" on:click|stopPropagation>
      <div class="nfo-modal-head">
        <div>
          <div class="nfo-modal-title">{@html ICO_DOC}NFO — {nfoModal.kind === 'torrents' ? 'Torrent' : nfoModal.kind === 'nzbs' ? 'NZB' : 'Lien DDL'} #{nfoModal.id}</div>
          {#if nfoModal.title}<div class="nfo-modal-sub">{nfoModal.title}</div>{/if}
        </div>
        <button class="btn-test" on:click={closeNfo}>✕ Fermer</button>
      </div>
      <div class="nfo-modal-body">
        {#if nfoModal.loading}
          <div style="color:var(--text3);font-size:12px">Chargement…</div>
        {:else if nfoModal.error}
          <div style="color:#ff9585;font-size:12px">{nfoModal.error}</div>
        {:else}
          {@html nfoModal.html}
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  /* Tokens, polices et atmosphère : voir src/style.css (source unique).
     Ce bloc ne garde que ce qui doit vivre au niveau du composant. */
  :global(*) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    margin: 0;
    font-family: var(--font-body);
    font-size: 13px;
    color: var(--text);
    background: var(--bg);
    height: 100vh;
    overflow: hidden;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
  }
  :global(input[type=text]),
  :global(input[type=password]),
  :global(input[type=number]),
  :global(input[type=email]),
  :global(select) {
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text);
    padding: 8px 11px;
    font: inherit;
    font-size: 13px;
    outline: none;
    width: 100%;
    transition: border-color 140ms, background 140ms;
  }
  :global(input:focus), :global(select:focus) {
    border-color: rgba(255, 107, 26, 0.45);
    background: rgba(255,255,255,0.05);
  }
  :global(button) { cursor: pointer; border: none; border-radius: 8px; font-size: 13px; font-weight: 500; transition: all 140ms ease; }
  :global(::-webkit-scrollbar) { width: 6px; height: 6px; }
  :global(::-webkit-scrollbar-track) { background: transparent; }
  :global(::-webkit-scrollbar-thumb) { background: rgba(255,255,255,0.1); border-radius: 3px; }
  :global(::-webkit-scrollbar-thumb:hover) { background: rgba(255,255,255,0.2); }

  .layout { display: flex; height: 100vh; position: relative; z-index: 2; }

  .sidebar {
    width: 200px; min-width: 200px;
    background: rgba(20, 16, 26, 0.6);
    backdrop-filter: blur(12px);
    border-right: 1px solid var(--border);
    display: flex; flex-direction: column;
    padding: 18px 12px;
    position: relative;
    transition: width 0.2s ease, min-width 0.2s ease, padding 0.2s ease;
    height: 100vh;
    overflow-y: auto;
    overflow-x: hidden;
    /* Scroll à la molette mais scrollbar invisible */
    scrollbar-width: none;
  }
  .sidebar::-webkit-scrollbar { display: none; }
  .sidebar.collapsed { width: 56px; min-width: 56px; padding: 18px 6px; }
  .sidebar.collapsed .brand { margin-bottom: 14px; }  .sidebar.collapsed .nav-item {
    padding: 9px 4px; justify-content: center; gap: 0;
    white-space: nowrap; overflow: hidden;
  }
  /* Le liseré se recentre quand la colonne est repliée. */
  .sidebar.collapsed .nav-item.active::before { left: -5px; }
  .sidebar.collapsed .btn-update { padding: 8px 4px; font-size: 14px; }

  .sidebar-toggle {
    position: absolute; top: 14px; right: -12px; z-index: 5;
    width: 24px; height: 24px; border-radius: 50%;
    background: var(--bg2); border: 1px solid var(--border);
    color: var(--text2); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    font-size: 16px; line-height: 1; font-weight: 700;
    box-shadow: 0 2px 8px rgba(0,0,0,0.3);
  }
  .sidebar-toggle:hover { background: var(--bg3); color: var(--text); }
  .brand {
    display: flex; flex-direction: column; align-items: center;
    gap: 10px; margin-bottom: 24px; padding: 4px 6px;
  }  /* Losange Elysium — version de la maquette v10. */
  .mark {
    width: 46px; height: 46px; position: relative; flex: none;
    background: linear-gradient(145deg, var(--ember-hi), var(--ember-deep));
    transform: rotate(45deg);
    border-radius: 9px;
    box-shadow: 0 0 26px var(--accent-glow), inset 0 1px 0 rgba(255,255,255,.35);
    margin: 2px auto 14px;
  }
  /* Creux central : carré assombri semi-opaque, pas un vrai trou — c'est ce
     qui garde la lecture « losange » plutôt qu'« anneau » (essayé, raté). */
  .mark::after {
    content: ""; position: absolute; inset: 11px;
    border-radius: 3px; background: var(--bg); opacity: .55;
  }
  .sidebar.collapsed .mark { width: 30px; height: 30px; margin: 2px auto 8px; }
  .sidebar.collapsed .mark::after { inset: 7px; }
  .wordmark {
    font-family: var(--font-display); font-size: 10px; font-weight: 700;
    letter-spacing: .26em; color: var(--ember);
    text-shadow: 0 0 14px var(--accent-glow);
  }
  .logo {
    font-family: var(--font-display);
    font-size: 12px; font-weight: 600;
    letter-spacing: .14em;
    text-transform: uppercase;
    color: var(--text);
    user-select: none;
    text-align: center;
  }
  nav { display: flex; flex-direction: column; gap: 3px; }
  .nav-item {
    position: relative;
    display: flex; align-items: center; gap: 11px;
    text-align: left; padding: 9px 13px; border-radius: 8px;
    background: transparent; color: var(--text2);
    font-family: var(--font-body); font-size: 12.5px; font-weight: 500;
    width: 100%; border: 0;
    transition: background 140ms ease, color 140ms ease;
  }
  .nav-icon { width: 16px; height: 16px; flex: none; }
  .nav-text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nav-item:hover { background: rgba(255,255,255,0.035); color: var(--text); }
  .nav-item.active { background: rgba(255, 107, 26, 0.09); color: var(--text); }
  /* Liseré braise collé au bord : repère l'onglet courant sans cerner
     le bouton d'une bordure, qui alourdirait la colonne. */
  .nav-item.active::before {
    content: ""; position: absolute; left: -9px; top: 50%;
    transform: translateY(-50%);
    width: 2px; height: 17px; border-radius: 0 2px 2px 0;
    background: var(--ember); box-shadow: 0 0 9px var(--accent-glow);
  }

  .content { flex: 1; overflow-y: auto; }

  .hist-stats { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:14px; }
  .hist-stat { padding:5px 12px; background:rgba(255,255,255,0.03); border:1px solid var(--border); border-radius:6px; font-size:11.5px; color:var(--text2); }
  .hist-stat b { color:var(--text1); margin-left:4px; font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
  .hist-stat.ok b { color:#22c55e; }
  .hist-stat.err b { color:#ef4444; }
  .hist-filters { display:flex; gap:10px; margin-bottom:14px; align-items:center; flex-wrap:wrap; }
  .hist-search-wrap { position:relative; display:flex; align-items:center; flex:1; min-width:260px; }
  .hist-search-wrap :global(.ui-ico) { position:absolute; left:11px; color:var(--text3); pointer-events:none; }
  .hist-search { flex:1; min-width:0; padding:8px 12px 8px 32px; background:rgba(255,255,255,0.04); border:1px solid rgba(255,255,255,0.1); border-radius:8px; color:var(--text1); font-size:13px; }
  .hist-type-btns { display:flex; gap:4px; }
  .hist-type-btns button { display:inline-flex; align-items:center; gap:6px; padding:6px 12px; background:rgba(255,255,255,0.04); border:1px solid rgba(255,255,255,0.08); border-radius:8px; color:var(--text2); font-size:12px; cursor:pointer; }
  .hist-type-btns button.active { background:rgba(255,90,60,0.15); border-color:#ff5a3c; color:#ff5a3c; }
  .hist-list { display:flex; flex-direction:column; gap:6px; max-height:calc(100vh - 260px); overflow-y:auto; }
  .hist-row { display:grid; grid-template-columns:110px 70px 1fr 80px 24px; gap:12px; padding:10px 14px; background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:8px; align-items:start; transition:border-color .14s, background .14s; }
  .hist-row:hover { border-color:rgba(255,107,26,0.3); background:rgba(255,107,26,0.03); }
  .hist-row.err { border-color:rgba(239,68,68,0.3); background:rgba(239,68,68,0.05); }
  .hist-col-date { font-size:11px; color:var(--text3); font-family:monospace; }
  .hist-col-type { font-size:10px; font-weight:700; letter-spacing:0.5px; padding:2px 6px; border-radius:4px; text-align:center; align-self:start; }
  .hist-type-torrent { background:rgba(34,197,94,0.15); color:#22c55e; }
  .hist-type-nzb { background:rgba(59,130,246,0.15); color:#3b82f6; }
  .hist-type-ddl { background:rgba(168,85,247,0.15); color:#a855f7; }
  .hist-col-main { min-width:0; }
  .hist-title { font-family: var(--font-display); font-size:13px; color:var(--text1); font-weight:600; }
  .hist-sub { font-size:11px; color:var(--text3); margin-top:2px; }
  .hist-links { font-size:10px; color:#00b4d8; margin-top:4px; word-break:break-all; white-space:pre-line; }
  .hist-error { display:flex; align-items:center; gap:5px; font-size:11px; color:var(--red-hot); margin-top:4px; }
  .hist-col-id { font-size:12px; color:var(--text2); font-family:monospace; }
  .hist-del { background:transparent; border:0; color:var(--text3); cursor:pointer; font-size:14px; padding:0; }
  .hist-del:hover { color:#ef4444; }
  .hist-empty { padding:40px; text-align:center; color:var(--text3); font-size:13px; background:rgba(255,255,255,0.02); border-radius:8px; }

  .modal-backdrop { position:fixed; inset:0; background:rgba(0,0,0,0.6); backdrop-filter:blur(6px); display:flex; align-items:center; justify-content:center; z-index:9999; }
  .modal-card { background:var(--bg3); border:1px solid rgba(255,255,255,0.1); border-radius:12px; width:min(90vw, 520px); box-shadow:0 20px 60px rgba(0,0,0,0.5); }
  .modal-header { display:flex; align-items:center; justify-content:space-between; padding:16px 20px; border-bottom:1px solid rgba(255,255,255,0.08); }
  .modal-header h3 { margin:0; font-size:16px; color:var(--text1); }
  .modal-close { background:transparent; border:0; color:var(--text3); font-size:18px; cursor:pointer; }
  .modal-close:hover { color:var(--text1); }
  .modal-body { padding:20px; color:var(--text2); font-size:13px; line-height:1.5; }
  .modal-body p { margin:0 0 8px; }
  .modal-hint { color:var(--text3); font-size:12px; }
  .modal-ok { color:#22c55e; }
  .modal-err { color:#ef4444; }
  .modal-body code { font-family:monospace; font-size:11px; background:rgba(255,255,255,0.05); padding:2px 6px; border-radius:4px; color:var(--text1); word-break:break-all; }
  .update-progress { margin-top:12px; }
  .update-bar { height:8px; background:rgba(255,255,255,0.06); border-radius:4px; overflow:hidden; }
  .update-bar-fill { height:100%; background:linear-gradient(90deg, #ff5a3c, #ff8b6b); transition:width 0.2s; }
  .update-msg { margin-top:8px; font-size:12px; color:var(--text3); font-family:monospace; }
  .modal-actions { display:flex; gap:10px; justify-content:flex-end; padding:14px 20px; border-top:1px solid rgba(255,255,255,0.08); }
  .btn-primary { background:#ff5a3c; border:0; color:white; padding:8px 18px; border-radius:8px; font-weight:600; cursor:pointer; font-size:13px; }
  .btn-primary:hover { background:#ff6b4f; }
  .btn-secondary { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.1); color:var(--text1); padding:8px 18px; border-radius:8px; cursor:pointer; font-size:13px; }
  .btn-secondary:hover { background:rgba(255,255,255,0.1); }

  .tab-content { padding: 28px 32px; max-width: 900px; }
  .tab-content h2 {
    font-size: 18px; font-weight: 700; margin-bottom: 22px;
    color: var(--text); letter-spacing: -0.01em;
    display: flex; align-items: center; gap: 9px;
  }
  .tab-content h2 :global(.ui-ico) { width: 18px; height: 18px; color: var(--ember); }

  .coming-soon {
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px dashed var(--border);
    border-radius: 14px; padding: 48px;
    text-align: center; color: var(--text3); font-size: 14px;
  }

  .sections { display: flex; flex-direction: column; gap: 16px; }

  /* Équerres de viseur sur deux coins opposés — même vocabulaire que
     les panneaux de l'écran Publier (voir HydrackerTab.svelte). */
  .section::before, .section::after {
    content: ""; position: absolute; width: 13px; height: 13px;
    border: 1.5px solid var(--ember); opacity: .45; pointer-events: none;
  }
  .section::before { top: -1px; left: -1px; border-right: 0; border-bottom: 0; border-radius: 14px 0 0 0; }
  .section::after { bottom: -1px; right: -1px; border-left: 0; border-top: 0; border-radius: 0 0 14px 0; }
  .section {
    position: relative;
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px solid var(--border);
    border-radius: 14px;
    padding: 18px 20px;
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.05),
      0 1px 2px rgba(0,0,0,0.4);
    animation: card-in 420ms cubic-bezier(0.16, 1, 0.3, 1) both;
  }
  @keyframes card-in {
    from { opacity: 0; transform: translateY(10px); }
    to   { opacity: 1; transform: translateY(0); }
  }
  .section-header {
    display: flex; align-items: center; justify-content: space-between;
    margin-bottom: 14px;
  }
  .section-header span { display: inline-flex; align-items: center; gap: 6px;
    font-family: var(--font-display);
    font-weight: 700; color: var(--text3); font-size: 9.5px;
    text-transform: uppercase; letter-spacing: .2em;
  }
  /* Le filet prend la largeur restante et pose une ligne de lecture. */
  .section-header { gap: 12px; }
  .section-header::after {
    content: ""; flex: 1; height: 1px; background: var(--border); order: 1;
  }
  .section-header > :not(span) { order: 2; }  @keyframes logo-in {
    from { opacity: 0; transform: scale(0.8) rotate(-6deg); }
    to   { opacity: 1; transform: scale(1) rotate(0deg); }
  }
  /* Section verrouillée team-shared (creds bakés au build) */
  .section-locked > .section-header {
    border-bottom: 2px solid #ff4444;
    padding-bottom: 8px;
  }
  .section-locked > .section-header span {
    color: #ff4444;
  }
  .section-locked input {
    opacity: 0.5;
    cursor: not-allowed;
    background: rgba(255, 68, 68, 0.04) !important;
    border-color: rgba(255, 68, 68, 0.2) !important;
  }
  .btn-test {
    background: rgba(255,255,255,0.04);
    color: var(--text2);
    border: 1px solid var(--border);
    padding: 6px 14px; font-size: 11px; font-weight: 500;
    text-transform: uppercase; letter-spacing: 0.4px;
    display: inline-flex; align-items: center; gap: 6px;
  }
  .btn-test:hover {
    background: rgba(255, 107, 26, 0.08);
    border-color: rgba(255, 107, 26, 0.35);
    color: var(--text);
  }
  .active-chip {
    background: rgba(255, 107, 26, 0.18) !important;
    border-color: rgba(255, 107, 26, 0.55) !important;
    color: var(--text) !important;
  }
  /* === Modale NFO === */
  .nfo-modal-bg {
    position: fixed; inset: 0; z-index: 1000;
    background: rgba(0,0,0,0.75);
    display: flex; align-items: center; justify-content: center;
    padding: 24px;
  }
  .nfo-modal-card {
    background: var(--bg2); border: 1px solid var(--border-strong);
    border-radius: 12px; max-width: 880px; width: 100%; max-height: 85vh;
    display: flex; flex-direction: column; overflow: hidden;
    box-shadow: 0 20px 60px rgba(0,0,0,0.5);
  }
  .nfo-modal-head {
    padding: 14px 18px; border-bottom: 1px solid var(--border);
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
  }
  .nfo-modal-title { font-size: 14px; font-weight: 600; color: var(--text); }
  .nfo-modal-sub { font-size: 11px; color: var(--text3); margin-top: 3px; word-break: break-all; }
  .nfo-modal-body {
    padding: 16px 18px; overflow-y: auto; flex: 1;
    font-family: ui-monospace, SFMono-Regular, Menlo, "Courier New", monospace;
    font-size: 11.5px; color: var(--text); line-height: 1.5;
    white-space: pre-wrap;
  }
  .nfo-modal-body :global(p) { margin: 0 0 2px; }
  .nfo-modal-body :global(a) { color: #60a5fa; text-decoration: none; word-break: break-all; }
  .nfo-modal-body :global(a:hover) { text-decoration: underline; }
  .nfo-modal-body :global(img) { max-width: 100%; height: auto; }
  /* ---------- Mes uploads ---------- */
  .chip-row { display: flex; gap: 6px; flex-wrap: wrap; }
  .up-user, .up-count { color: var(--text3); }
  .up-user b, .up-count b { color: var(--text2); }
  .up-pager { display: flex; align-items: center; gap: 6px; }
  .up-pager span {
    font-size: 10.5px; color: var(--text3);
    font-variant-numeric: tabular-nums;
  }
  .up-pager .btn-test { padding: 4px 9px; }

  .up-state {
    padding: 18px; text-align: center; font-size: 12px; color: var(--text3);
    border: 1px dashed var(--border); border-radius: 10px;
  }
  .up-state-err {
    color: var(--red-hot); border-color: rgba(255,77,94,.3);
    display: flex; align-items: center; justify-content: center; gap: 7px;
  }

  /* Une ligne = une release. Révélation décalée : les premières arrivent
     d'abord, plafonnée à 14 pour qu'une grande page ne traîne pas. */
  .up-row {
    display: flex; gap: 14px; align-items: center;
    padding: 11px 13px; margin-bottom: 7px;
    background: rgba(255,255,255,0.025);
    border: 1px solid var(--border);
    border-left: 2px solid rgba(255,107,26,.3);
    border-radius: 9px;
    transition: border-color 140ms ease, background 140ms ease, transform 140ms ease;
    animation: card-in 380ms cubic-bezier(0.16, 1, 0.3, 1) both;
  }
  .up-row:hover {
    background: rgba(255,107,26,0.045);
    border-color: rgba(255,107,26,.3);
    border-left-color: var(--ember);
    transform: translateX(2px);
  }
  .up-main { flex: 1; min-width: 0; }
  .up-title { display: flex; align-items: baseline; gap: 8px; margin-bottom: 5px; }
  .up-id {
    font-size: 10.5px; color: var(--ember-hi); flex: none;
    letter-spacing: .02em;
  }
  .up-name {
    font-size: 12.5px; font-weight: 600; color: var(--text1);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .up-meta { display: flex; gap: 6px; flex-wrap: wrap; }
  .up-chip {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 2px 7px; border-radius: 4px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    font-size: 10px; color: var(--text3);
  }
  .up-chip b { color: var(--text2); font-weight: 600; }
  .up-chip-warn {
    color: var(--warning); border-color: rgba(255,200,61,.3);
    background: rgba(255,200,61,.07);
  }
  .up-actions { display: flex; flex-direction: column; gap: 5px; flex: none; }
  .up-actions .btn-test { padding: 5px 11px; }

  /* Rotation continue du picto pendant un chargement — transform seul, pas de
     filtre : la règle de perf du cockpit s'applique aussi ici. */
  /* :global — le picto vient de {@html}, il n'a pas la classe de scope. */
  .up-spinning :global(.ui-ico) { animation: up-spin 900ms linear infinite; }
  @keyframes up-spin { to { transform: rotate(360deg); } }

  .btn-danger { color: var(--red-hot) !important; border-color: rgba(255,77,94,.32) !important; }
  .btn-danger:hover { background: rgba(255,77,94,.1) !important; border-color: rgba(255,77,94,.6) !important; }
  .modal-card-danger { border-color: rgba(255,77,94,.5); }
  .modal-title-danger { color: var(--red-hot); }
  .up-del-box {
    background: rgba(0,0,0,0.28); border: 1px solid var(--border);
    padding: 10px 12px; border-radius: 8px; margin: 10px 0; font-size: 12px;
  }
  .up-del-sub { color: var(--text3); font-size: 11px; margin-top: 4px; }
  .btn-save-danger {
    background: linear-gradient(180deg, #ff6b7a 0%, #e03346 100%) !important;
    border-color: #ff4d5e !important;
  }

  .modal-backdrop {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.6);
    backdrop-filter: blur(4px);
    display: flex; align-items: center; justify-content: center;
    z-index: 1000;
  }
  .modal-card {
    background: var(--paper, #1e1e24);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px 24px;
    max-width: 480px;
    width: 90%;
    max-height: 85vh;
    overflow-y: auto;
  }
  .modal-title { font-family: var(--font-display); font-size: 15px; font-weight: 600; color: var(--text); margin-bottom: 4px; letter-spacing: .04em; }
  .modal-title { display: flex; align-items: center; gap: 8px; }
  .modal-hint { color: var(--text3); font-size: 12px; margin-bottom: 12px; }
  .api-log-row {
    font-family: var(--font-mono); font-size: 11px;
    padding: 8px 10px;
    border-bottom: 1px solid rgba(255,255,255,0.04);
    cursor: pointer;
    transition: background 0.1s;
  }
  .api-log-row:hover { background: rgba(255,255,255,0.02); }
  .api-log-err { background: rgba(255,107,107,0.04); }
  .api-log-method {
    display: inline-block;
    padding: 1px 8px;
    border-radius: 4px;
    font-weight: 700;
    font-size: 10px;
    letter-spacing: 0.5px;
    min-width: 50px;
    text-align: center;
  }
  .api-log-method-GET    { background: rgba(126,240,192,0.18); color: #7ef0c0; }
  .api-log-method-POST   { background: rgba(0,180,216,0.18);   color: #00b4d8; }
  .api-log-method-PUT    { background: rgba(255,214,10,0.18);  color: #ffd60a; }
  .api-log-method-PATCH  { background: rgba(255,214,10,0.18);  color: #ffd60a; }
  .api-log-method-DELETE { background: rgba(255,107,107,0.18); color: #ff6b6b; }

  /* ---------- Logs API : une ligne = une requête ---------- */
  .api-log-head { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; font-size: 11px; }
  .api-log-ts   { color: var(--text3); flex: none; }
  .api-log-dur  { color: var(--text3); flex: none; }
  .api-log-status { font-weight: 700; min-width: 38px; color: var(--text3); flex: none; }
  .api-st-ok  { color: var(--success); }
  .api-st-err { color: var(--red-hot); }
  .api-log-url { flex: 1; min-width: 0; color: var(--text2); word-break: break-all; }
  .api-log-error {
    display: flex; align-items: center; gap: 6px;
    color: var(--red-hot); font-size: 11px; margin-top: 5px;
  }
  /* Corps de réponse : replié par défaut, borné en hauteur pour qu'un gros
     JSON ne pousse pas le reste de la liste hors de l'écran. */
  .api-log-body {
    background: rgba(0,0,0,0.32); border: 1px solid var(--border);
    padding: 9px 10px; border-radius: 6px; margin-top: 7px;
    font-family: var(--font-mono); font-size: 10px; color: var(--success);
    white-space: pre-wrap; max-height: 240px; overflow: auto;
  }
  .api-filters { margin-bottom: 10px; }

  /* ---------- Réglages ---------- */
  .settings-group {
    display: flex; align-items: center; gap: 7px;
    font-family: var(--font-display);
    font-size: 9.5px; font-weight: 700; color: var(--text3);
    text-transform: uppercase; letter-spacing: .2em;
    margin: 0 0 9px;
  }
  .settings-group :global(.ui-ico) { color: var(--ember); }
  .settings-group-spaced { margin-top: 20px; }  .settings-note-txt :global(.ui-ico) { color: var(--success); }

  .settings-hint { color: var(--text3); font-size: 12px; line-height: 1.5; margin-bottom: 8px; }
  .settings-hint-ok { display: inline-flex; align-items: center; gap: 6px; }
  .settings-hint-ok :global(.ui-ico) { color: var(--success); }

  .c1f-row { display: flex; gap: 6px; align-items: center; margin-bottom: 6px; }
  .c1f-row input { flex: 1; min-width: 0; }
  .c1f-label  { max-width: 150px; }
  .c1f-folder { max-width: 190px; }
  .c1f-del { padding: 6px 9px !important; flex: none; }
  .c1f-alerte {
    font-size: 11px; color: var(--warning); line-height: 1.45;
    background: rgba(255,200,61,.07); border: 1px solid rgba(255,200,61,.3);
    border-radius: 7px; padding: 7px 10px; margin: -2px 0 8px;
  }

  .outil {
    display: flex; align-items: baseline; gap: 12px;
    padding: 6px 0; border-bottom: 1px solid var(--border);
    font-size: 12px;
  }
  .outil:last-of-type { border-bottom: 0; }
  .outil-nom { color: var(--text1); font-weight: 600; min-width: 92px; flex: none; }
  .outil-src { color: var(--success); font-size: 11px; }
  .outil-ver {
    margin-left: auto; color: var(--text3); font-size: 10.5px;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .outil-warn .outil-src { color: var(--warning); }
  .outil-err  .outil-src { color: var(--red-hot); }
  .outil-detail {
    font-family: var(--font-mono); font-size: 10px; color: var(--text3);
    padding: 0 0 6px; word-break: break-all;
  }
  .api-search { margin-bottom: 0; }

  .test-result {
    font-size: 12px; padding: 7px 11px; border-radius: 8px; margin-bottom: 12px;
    background: rgba(239, 68, 68, 0.08); color: #ff9585;
    border: 1px solid rgba(239, 68, 68, 0.25);
  }
  .test-result.ok {
    background: rgba(34, 197, 94, 0.08); color: #7ef0c0;
    border-color: rgba(34, 197, 94, 0.25);
  }

  .field { margin-bottom: 12px; }
  .field:last-child { margin-bottom: 0; }
  .field label { display: block; font-size: 11px; color: var(--text3); margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.6px; }
  .field-checkbox label { display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: 13px; color: var(--text); margin-top: 8px; text-transform: none; letter-spacing: 0; }
  .field-hint { font-size: 11px; color: var(--text3); margin-top: 4px; }
  .field-hint a { color: var(--accent); text-decoration: none; border-bottom: 1px solid rgba(255,107,26,.3); }
  .field-hint a:hover { border-bottom-color: var(--ember); }  .field-checkbox input[type=checkbox] { width: auto; }

  .fields-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }

  .btn-save {
    margin-top: 24px;
    display: inline-flex; align-items: center; gap: 8px;
    color: #fff;
    background: var(--grad-primary);
    border: 1px solid rgba(0,0,0,0.25);
    padding: 11px 28px; font-size: 14px; font-weight: 600;
    letter-spacing: 0.2px;
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.25),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 1px 2px rgba(0,0,0,0.4),
      0 8px 24px -6px var(--accent-glow);
  }
  .btn-save:hover {
    background: var(--grad-primary-hover);
    filter: brightness(1.05);
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.3),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 2px 6px rgba(0,0,0,0.5),
      0 12px 32px -4px var(--accent-glow);
  }
  .btn-save:active { transform: translateY(1px); filter: brightness(0.95); }.log-list { display: flex; flex-direction: column; gap: 3px; font-size: 12px; font-family: var(--font-mono); }
  .log-line {
    display: flex; gap: 10px; align-items: baseline;
    padding: 5px 10px;
    background: rgba(255,255,255,0.02);
    border: 1px solid var(--border);
    border-radius: 6px;
  }
  .log-ts { color: var(--text3); flex: none; }
  .log-prefix { flex: none; font-weight: 700; padding: 2px 7px; border-radius: 4px; font-size: 10px; letter-spacing: 0.4px; }
  .log-prefix-ddl { background: rgba(0, 180, 216, 0.15); color: var(--blue-hot); }
  .log-prefix-nzb { background: rgba(247, 127, 0, 0.15); color: var(--orange); }
  .log-prefix-mi  { background: rgba(34, 197, 94, 0.15); color: #7ef0c0; }
  .log-prefix-meta{ background: rgba(255, 214, 10, 0.15); color: var(--yellow); }
  .log-prefix-tor { background: rgba(230, 57, 70, 0.18); color: var(--red-hot); }
  .log-msg { color: var(--text); word-break: break-all; }
  .log-prefix-chk { background: rgba(72, 202, 228, 0.18); color: var(--blue-hot); }
  .log-prefix-res { background: rgba(247, 127, 0, 0.15); color: var(--orange); }
  .log-prefix-watch { background: rgba(255, 214, 10, 0.18); color: var(--yellow); }
  .log-prefix-queue { background: rgba(255, 214, 10, 0.22); color: #ffe066; font-weight: 700; }

  /* ---------- Journal : filtres, recherche, sévérité ---------- */
  .log-count { color: var(--text3); }
  .log-count b { color: var(--ember-hi); }

  .log-search { position: relative; display: flex; align-items: center; margin-bottom: 10px; }
  .log-search :global(.ui-ico) {
    position: absolute; left: 11px; color: var(--text3); pointer-events: none;
  }
  .log-search input {
    width: 100%; padding-left: 32px; padding-right: 32px;
    font-family: var(--font-mono); font-size: 12px;
  }
  .log-search-x {
    position: absolute; right: 8px;
    background: none; border: none; color: var(--text3);
    cursor: pointer; font-size: 13px; line-height: 1; padding: 4px 6px;
  }
  .log-search-x:hover { color: var(--text1); }

  .log-tags { display: flex; gap: 5px; flex-wrap: wrap; margin-bottom: 10px; }
  .log-tag {
    font-family: var(--font-mono); font-size: 10px; font-weight: 700;
    letter-spacing: .05em; padding: 3px 8px; border-radius: 4px;
    background: rgba(255,255,255,0.06); color: var(--text2);
    border: 1px solid transparent; cursor: pointer;
    transition: opacity 120ms ease, filter 120ms ease;
  }
  .log-tag:hover { filter: brightness(1.25); }
  /* Masqué : on garde le tag lisible mais visiblement éteint, pour qu'on voie
     d'un coup d'œil ce qu'on a mis de côté. */
  .log-tag-off {
    opacity: .3; background: rgba(255,255,255,0.03) !important;
    color: var(--text3) !important;
    border-color: var(--border); text-decoration: line-through;
  }
  .log-tag-reset { padding: 3px 9px !important; font-size: 9.5px !important; }

  .log-actions { display: flex; gap: 6px; }

  /* Un échec se repère au liseré, pas à la couleur du texte : la ligne reste
     lisible quand on la copie, et l'œil accroche en balayant la colonne. */
  .log-line-err  { border-left: 2px solid var(--danger); background: rgba(255,77,94,.05); }
  .log-line-warn { border-left: 2px solid var(--warning); background: rgba(255,200,61,.04); }
  .brand-version {
    font-size: 10px; color: var(--text3);
    font-family: ui-monospace, Menlo, monospace;
    letter-spacing: 0.6px; user-select: none;
  }
  .brand-author {
    font-size: 9px; color: var(--text3); opacity: 0.75;
    letter-spacing: 1.2px; text-transform: uppercase;
    margin-top: 2px; user-select: none;
  }
  .sidebar-footer { margin-top: auto; padding-top: 14px; }
  .btn-update {
    width: 100%;
    background: var(--grad-primary);
    color: #fff;
    border: 1px solid rgba(0,0,0,0.25);
    padding: 9px 12px;
    font-size: 11px; font-weight: 700;
    letter-spacing: 0.5px;
    border-radius: 8px;
    cursor: pointer;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.25), 0 6px 18px -6px var(--accent-glow);
    animation: pulse-update 2.4s ease-in-out infinite;
  }
  .btn-update:hover { filter: brightness(1.1); }

  .btn-check-update {
    width: 100%;
    background: rgba(255,255,255,0.04);
    color: var(--text3);
    border: 1px solid rgba(255,255,255,0.08);
    padding: 7px 10px;
    font-size: 11px; font-weight: 500;
    border-radius: 8px;
    cursor: pointer;
    display: flex; align-items: center; gap: 6px; justify-content: center;
  }
  .btn-check-update:hover { background: rgba(255,255,255,0.08); color: var(--text); }
  .btn-check-update:disabled { opacity: 0.5; cursor: not-allowed; }
  .sidebar.collapsed .btn-check-update { padding: 7px 4px; }@keyframes nexum-float {
    0%, 100% { transform: translateY(0); }
    50% { transform: translateY(-8px); }
  }@keyframes bulk-slide { from { opacity: 0; transform: translateY(-6px); } to { opacity: 1; transform: translateY(0); } }@keyframes detail-slide { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }  /* Sous-onglets (Reseed, Logs) */
  .sub-tabs-nav {
    display: flex; gap: 4px;
    margin: -6px 0 18px;
    padding: 4px;
    background: rgba(255,255,255,0.03);
    border-radius: 10px;
    width: fit-content;
  }
  .sub-tabs-nav button { display: inline-flex; align-items: center; gap: 7px;
    background: transparent; border: 0;
    color: var(--text3);
    padding: 7px 14px; font-size: 12px; font-weight: 600;
    border-radius: 7px; cursor: pointer;
    transition: all 0.2s ease;
  }
  .sub-tabs-nav button:hover { color: var(--text); background: rgba(255,255,255,0.04); }
  .sub-tabs-nav button.active {
    background: var(--bg2);
    color: var(--text);
    box-shadow: 0 2px 8px -2px rgba(0,0,0,0.4), 0 0 0 1px var(--border);
  }@keyframes modal-fade { from { opacity: 0; } to { opacity: 1; } }  @keyframes modal-slide {
    from { transform: translateY(12px) scale(0.98); opacity: 0; }
    to   { transform: translateY(0) scale(1); opacity: 1; }
  }  @keyframes pulse-update {
    0%, 100% { box-shadow: inset 0 1px 0 rgba(255,255,255,0.25), 0 6px 18px -6px var(--accent-glow); }
    50%      { box-shadow: inset 0 1px 0 rgba(255,255,255,0.3),  0 10px 28px -4px var(--accent-glow); }
  }
  .log-prefix-fic { background: rgba(230, 57, 70, 0.18); color: var(--red-hot); }</style>
