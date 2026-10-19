<script>
  import { onMount, onDestroy } from 'svelte'
  import { EventsOn, EventsOff, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime.js'
  import { ParseFilename, TMDBSearch, TMDBGetByID, IgdbSearch, IgdbGetByID, OpenBrowser, SelectMkvFile, SelectMkvFiles, SelectFolder, SelectArchiveFile, PrepareSeasonFolder, FindFirstMkvInFolder, PostNzbWorkflow, PostDDLWorkflow, FetchImageBase64, GetFileSize, ReadFileChunk, MediaInfoNative, MediaSearch, ListMyElyNzbs, ListMyElyDdls, CancelAllWorkflows, Notify, CancelDDLHost, SkipCurrentEpisode, GetVersion, GetElysiumMeta, ElysiumSearchTitles, ElysiumGetTitleByTmdbID, ElysiumGetTitleByIgdbID, ElysiumImportTitle , OneFichierChoices, ActiveOneFichierLabel, SetActiveOneFichier} from '../wailsjs/go/main/App.js'

  // Adapter Elysium Title → forme "PartialTitle Hydracker" attendue par le reste
  // de la UI (variable historique `selectedHydracker`). L'app a pivoté sur Elysium
  // le 2026-09-03 (Hydracker fermé) — les noms de variables restent inchangés
  // pour minimiser la diff, avec Elysium comme source de vérité.
  function adaptElyTitle(t) {
    if (!t) return null
    const year = t.year ? String(t.year) : ''
    return {
      id: t.id,
      name: t.title,
      type: t.type || 'movie',
      release_date: year ? `${year}-01-01` : '',
      tmdb_id: t.tmdb_id || 0,
      igdb_id: t.igdb_id || 0,
      poster: t.poster_url || '', // URL absolue https://image.tmdb.org/t/p/w185/...
    }
  }

  async function HydrackerSearch(query) {
    const results = await ElysiumSearchTitles(query || '', '') || []
    return results.map(adaptElyTitle)
  }
  // mediaType est CRITIQUE pour désambiguïser tmdb_id : TMDB réutilise le même
  // id entre movie et tv (ex 615 = Passion du Christ film ET Futurama série).
  async function HydrackerGetByTmdbID(tmdbID, mediaType = '') {
    const mt = mediaType || selectedTMDB?.media_type || ''
    let t = await ElysiumGetTitleByTmdbID(tmdbID, mt)
    if (!t) {
      // Fiche pas encore sur Elysium — import auto depuis TMDB.
      try { t = await ElysiumImportTitle(mt, tmdbID) } catch (_) { t = null }
    }
    return adaptElyTitle(t)
  }
  async function HydrackerGetByIgdbID(igdbID) {
    let t = await ElysiumGetTitleByIgdbID(igdbID)
    if (!t) {
      try { t = await ElysiumImportTitle('game', igdbID) } catch (_) { t = null }
    }
    return adaptElyTitle(t)
  }
  // HydrackerGetByID (recherche par ID Hydracker interne) n'a plus de sens
  // post-pivot : on retombe sur un lookup TMDB puisque l'user tape un tmdb_id.
  async function HydrackerGetByID(id) {
    return HydrackerGetByTmdbID(id)
  }
  import { addLog } from './logs.js'
  import { LANGUAGES as HYD_LANGUAGES, SUBS as HYD_SUBS, QUALITIES as HYD_QUALITIES } from './hydrackerData.js'

  // --- State ---
  let dragOver = false
  let file = null
  let fileInfo = null
  let mediaInfo = null
  let mediaInfoLoading = false
  let mediaInfoError = ''

  // TMDB
  let tmdbResults = []
  let tmdbAmbiguous = false
  let rechercheMessage = ''   // message visible quand la recherche ne donne rien
  let selectedTMDB = null
  let tmdbSearchQuery = ''
  let tmdbSearchId = ''
  let tmdbSearchType = 'movie'  // 'movie' | 'tv' | 'game' — toggle pour la recherche manuelle
  let igdbResults = []          // résultats recherche IGDB (mode Jeu)
  let elysiumStatus = null      // {status: 'start'|'uploading'|'ok'|'error'|'skip', msg}
  // Toggles cross-post : par défaut on poste sur Hydracker (comportement historique)
  // et sur Elysium si le token est configuré.
  let postTargets = { hydracker: true, elysium: true }
  $: gameMode = tmdbSearchType === 'game'
  let tmdbSearchLoading = false

  // Poster TMDB
  let posterDataUrl = ''
  // Poster Hydracker
  let hydrackerPosterUrl = ''

  // Hydracker
  let hydrackerResults = []
  let hydrackerSearchCache = []   // derniers résultats pour restaurer après désélection
  let hydrackerSearchQuery = ''
  let hydrackerSearchLoading = false
  let selectedHydracker = null
  let hydrackerNotFound = false      // fiche introuvable après recherche auto
  let hydrackerManualId = ''         // saisie manuelle de l'ID Hydracker
  let hydrackerManualType = 'tv'     // 'tv' ou 'movie' — sert à choisir le bon endpoint TMDB lors du resync
  let manualTmdbIdEdit = ''          // saisie manuelle TMDB ID dans la card "introuvable"
  let tmdbReloadLoading = false      // spinner du bouton 🔄 reload TMDB
  // Sync l'input avec le selectedTMDB courant pour éviter d'afficher l'ID
  // d'un précédent post quand on bascule de fichier.
  $: manualTmdbIdEdit = selectedTMDB?.id ? String(selectedTMDB.id) : manualTmdbIdEdit
  let hydrackerManualLoading = false

  // Hydracker post fields
  let postQuality = ''  // nom de qualité Elysium (ex: "WEB 1080p (x265)")
  let postLanguages = []   // [{id, name}]
  let postSubs = []        // [{id, name}]
  let langsAutoFilled = false
  let subsAutoFilled = false
  let qualityAutoFilled = false  // true tant que l'user n'a pas changé manuellement la qualité
  let queueQualityHint = ''      // qualité sticky (nom Elysium) dans une queue série
  // Langues / sous-titres sticky. null = jamais touché par l'user → on
  // laisse l'auto-détection MediaInfo faire son travail épisode par
  // épisode. Dès qu'il corrige EP1, sa correction devient la référence
  // pour tous les suivants : c'est presque toujours le même encodeur sur
  // une saison, donc si l'auto-détection se trompe sur EP1 elle se
  // trompera pareil sur EP2.
  let queueLangHint = null
  let queueSubHint = null
  let postUploadTypes = { nzb: false, ddl: false }

  // Comptes 1Fichier : plusieurs clés possibles, une seule sert par post.
  // Le choix est mémorisé côté Go, donc il survit au redémarrage.
  let comptes1F = []
  let compte1FActif = ''
  async function chargerComptes1F() {
    try {
      comptes1F = await OneFichierChoices() || []
      compte1FActif = await ActiveOneFichierLabel() || ''
    } catch (e) { addLog('DDL', '⚠ comptes 1Fichier : ' + e) }
  }
  async function changerCompte1F(label) {
    try {
      await SetActiveOneFichier(label)
      compte1FActif = label
      addLog('DDL', `compte 1Fichier : « ${label} »`)
    } catch (e) { addLog('DDL', '✗ ' + e) }
  }
  // Torrent ADMIN n'est visible que pour les admins (= ceux qui ont déverrouillé

  // Send.now retiré 2026-09-04 : Elysium n'accepte que 1Fichier, unique host DDL.
  let postSeason = 0
  let postEpisode = 0
  // Anchor de numérotation manuelle en queue (typique anime numéroté 001, 002…
  // sans saison où l'user re-mappe : fichier ...062 → S02E01). Une fois posé,
  // les fichiers suivants héritent : rawEp N > anchor.rawEp ⇒ postSeason =
  // anchor.saison, postEpisode = anchor.episode + (N - anchor.rawEp).
  let queueEpisodeAnchor = null  // { rawEp: int, saison: int, episode: int } | null
  let postFullSaison = false  // toggle "Saison complète" — désactive le numéro d'épisode
  let postNfoManual = ''      // NFO custom écrit par l'user (override la génération auto)
  let nfoEditMode = false     // true = textarea éditable, false = preview readonly
  let seasonMenuOpen = false  // (legacy, plus utilisé)
  let pendingMultiPaths = null  // array des paths quand l'user a sélectionné plusieurs MKV — choisir queue ou saison

  // Fichiers sélectionnés pour l'upload
  let mkvFilePath = ''
  let dropDebounce = null  // debounce le loadFileFromPath après OnFileDrop pour ordre garanti
  let mediaInfoOpen = true   // MediaInfo dépliée par défaut
  let nfoOpen = false        // NFO repliée : déplier poussait la fenêtre
                             // loin vers le bas dès la sélection d'un fichier
  let recapOpen = true       // Récapitulatif déplié par défaut

  // Résultat du post
  let postLoading = false
  let postResult = null  // { ok: bool, message: string, details: string }

  // Progression NZB en temps réel
  let nzbStatus = ''
  let nzbParparPct = 0
  let nzbNyuuPct = 0

  // Phases d'un post NZB, dans l'ordre. Le Go émet `nzb:status` à chaque
  // étape (« Génération PAR2… », « Post Usenet… », « Upload NZB sur
  // Elysium… », « Terminé ») : on en déduit l'avancement plutôt que de
  // maintenir un état parallèle qui pourrait désynchroniser.
  // Pictos SVG inlinés pour les boutons compacts (copier / éditer / voir).
  // Constantes plutôt que balisage répété : ces boutons apparaissent à
  // plusieurs endroits et doivent rester identiques.
  const _ico = d => `<svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor"`
    + ` stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="${d}"/></svg>`
  const COPY_SVG = _ico('M9 9V6.75A1.5 1.5 0 0 1 10.5 5.25h6.75A1.5 1.5 0 0 1 18.75 6.75v6.75a1.5 1.5 0 0 1-1.5 1.5H15M6.75 9h6.75a1.5 1.5 0 0 1 1.5 1.5v6.75a1.5 1.5 0 0 1-1.5 1.5H6.75a1.5 1.5 0 0 1-1.5-1.5V10.5A1.5 1.5 0 0 1 6.75 9Z')
  const PEN_SVG  = _ico('M16.5 4.5l3 3L8.25 18.75 4.5 19.5l.75-3.75L16.5 4.5Z')
  const EYE_SVG  = _ico('M2.25 12s3.75-6.75 9.75-6.75S21.75 12 21.75 12s-3.75 6.75-9.75 6.75S2.25 12 2.25 12Zm9.75 2.625a2.625 2.625 0 1 0 0-5.25 2.625 2.625 0 0 0 0 5.25Z')

  const POST_PHASES = ['ANALYSE', 'PAR2', 'USENET', 'ELYSIUM']

  // ------------------------------------------------------------------
  // Panneau de suivi (à droite des barres) : étapes, chiffres, courbe.
  // Tout est DÉDUIT de l'état réel du workflow — aucune étape décorative,
  // sinon le panneau mentirait sur ce qui se passe.
  // ------------------------------------------------------------------
  let postStartedAt = 0
  let postElapsed = 0
  let debitSeries = []     // [{ddl, nzb}] un point par seconde, 60 max
  let debitTimer = null

  // "63.0 MB/s", "32.36 MiB/s", "812 KiB/s" → Mo/s. nyuu parle en MiB,
  // 1Fichier en MB : on ramène tout à la même unité avant d'additionner.
  function parseDebit(txt) {
    const m = /([\d.]+)\s*([KMG]i?B)\/s/i.exec(txt || '')
    if (!m) return 0
    const v = parseFloat(m[1]) || 0
    const u = m[2].toLowerCase()
    if (u.startsWith('k')) return v / 1024
    if (u.startsWith('g')) return v * 1024
    return v
  }
  $: ddlDebit = parseDebit(ddlHosts['1Fichier']?.speed)
  $: nzbDebit = parseDebit(nzbNyuuSpeed)
  $: debitTotal = ddlDebit + nzbDebit

  function fmtDuree(sec) {
    if (!sec || sec < 0 || !isFinite(sec)) return '—'
    sec = Math.round(sec)
    const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), r = sec % 60
    return h ? `${h} h ${String(m).padStart(2, '0')}` : `${m}:${String(r).padStart(2, '0')}`
  }
  function fmtGo(octets) {
    if (!octets) return '0'
    return octets >= 1e9 ? (octets / 1e9).toFixed(2) + ' Go' : Math.round(octets / 1e6) + ' Mo'
  }

  // Volume : le NZB envoie le fichier PLUS la redondance PAR2 (~10 %), le DDL
  // envoie le fichier seul. D'où un total qui dépend des cases cochées.
  $: tailleFichier = mediaInfo?.filesizeBytes || 0
  $: volumeTotal = tailleFichier * ((postUploadTypes.ddl ? 1 : 0) + (postUploadTypes.nzb ? 1.1 : 0))
  $: volumeEnvoye = tailleFichier * (
       (postUploadTypes.ddl ? (ddlHosts['1Fichier']?.pct || 0) / 100 : 0) +
       (postUploadTypes.nzb ? 1.1 * (nzbNyuuPct || 0) / 100 : 0))

  // Restant : l'ETA de nyuu quand il existe (c'est lui qui sait), sinon une
  // extrapolation sur l'avancement écoulé.
  $: resteEstime = (() => {
    if (nzbNyuuETA) return nzbNyuuETA
    const pct = Math.max(ddlHosts['1Fichier']?.pct || 0, nzbNyuuPct || 0)
    if (!postElapsed || pct <= 1) return '—'
    return fmtDuree(postElapsed * (100 - pct) / pct)
  })()

  const ETAT_OK = 'ok', ETAT_NOW = 'now', ETAT_WAIT = 'wait', ETAT_ERR = 'err'
  $: postSteps = (() => {
    const l = []
    l.push({ t: 'Analyse du fichier', e: mediaInfo ? ETAT_OK : (file ? ETAT_NOW : ETAT_WAIT) })
    l.push({ t: selectedHydracker ? `Fiche Elysium #${selectedHydracker.id}` : 'Fiche Elysium',
             e: selectedHydracker ? ETAT_OK : (selectedTMDB ? ETAT_NOW : ETAT_WAIT) })
    l.push({ t: 'NFO généré', e: nfoPreview ? ETAT_OK : ETAT_WAIT })
    if (postUploadTypes.ddl) {
      const h = ddlHosts['1Fichier'] || {}
      l.push({ t: 'Upload 1Fichier', pct: h.pct || 0,
               e: h.error ? ETAT_ERR : ((h.posted || h.done) ? ETAT_OK : (h.active ? ETAT_NOW : ETAT_WAIT)) })
    }
    if (postUploadTypes.nzb) {
      l.push({ t: 'Génération PAR2', pct: nzbParparPct,
               e: postPhase > 1 ? ETAT_OK : (postPhase === 1 ? ETAT_NOW : ETAT_WAIT) })
      l.push({ t: 'Post Usenet', pct: nzbNyuuPct,
               e: postPhase > 2 ? ETAT_OK : (postPhase === 2 ? ETAT_NOW : ETAT_WAIT) })
    }
    l.push({ t: 'Publication Elysium',
             e: postResult?.ok ? ETAT_OK
               : ((postPhase === 3 || ddlHosts['1Fichier']?.posting) ? ETAT_NOW : ETAT_WAIT) })
    return l
  })()

  // Échantillonnage à la seconde pour la courbe + le chrono.
  $: if (postLoading && !debitTimer) {
    postStartedAt = Date.now()
    postElapsed = 0
    debitSeries = []
    debitTimer = setInterval(() => {
      postElapsed = Math.round((Date.now() - postStartedAt) / 1000)
      debitSeries = [...debitSeries, { ddl: ddlDebit, nzb: nzbDebit }].slice(-60)
    }, 1000)
  } else if (!postLoading && debitTimer) {
    clearInterval(debitTimer)
    debitTimer = null
  }

  // Courbe : une polyligne par flux, normalisée sur le maximum observé pour
  // que le relief reste lisible quel que soit le débit.
  const COURBE_W = 100, COURBE_H = 26
  $: debitMax = Math.max(1, ...debitSeries.map(p => Math.max(p.ddl, p.nzb)))
  function polyligne(cle) {
    if (debitSeries.length < 2) return ''
    const n = debitSeries.length
    return debitSeries.map((p, i) =>
      `${(i / (n - 1) * COURBE_W).toFixed(1)},${(COURBE_H - (p[cle] / debitMax) * COURBE_H).toFixed(1)}`
    ).join(' ')
  }
  $: courbeDdl = debitSeries.length ? polyligne('ddl') : ''
  $: courbeNzb = debitSeries.length ? polyligne('nzb') : ''

  $: postEnCours = postLoading || !!nzbStatus ||
      Object.values(ddlHosts).some(h => h.active || h.done || h.error || h.posting)

  // Nombre de phases terminées = index de la phase en cours.
  $: postPhase = (() => {
    const s = (nzbStatus || '').toLowerCase()
    if (s.includes('terminé') || s.includes('termine')) return 4
    if (s.includes('elysium')) return 3
    if (s.includes('usenet')) return 2
    if (s.includes('par2')) return 1
    return 0
  })()
  let nzbNyuuSpeed = ''
  let nzbNyuuETA = ''
  let nzbNyuuArticles = ''

  // Progression DDL — 1Fichier seul depuis 2026-09-04.
  let ddlHosts = {
    '1Fichier': { active: false, filename: '', pct: 0, speed: '', done: false, posting: false, posted: false, hydrackerID: 0, error: '' },
  }

  // Meta depuis l'API Hydracker
  let qualityOptions = []  // [{id, name}]
  let appVersion = ''      // rempli au onMount via GetVersion()
  let langOptions = []     // [{id, name}]
  let subOptions = []      // [{id, name}] — liste spécifique aux sous-titres

  onMount(async () => {
    chargerComptes1F()
    try { appVersion = await GetVersion() } catch(e) { appVersion = '' }
    // Chargement meta Elysium — remplacement direct des ex-appels
    // GetMetaQualities/Langs/Subs (Hydracker fermé 2026-09-03).
    // meta.qualities/languages/subtitles sont des map {key: label} → on les
    // convertit en [{id, name}] avec id=name : Elysium indexe par NAME dans
    // /upload, et cette égalité garde compatible tout le code historique qui
    // stocke un « id » dans postQuality/postLanguages/postSubs.
    const mapToOpts = m => Object.keys(m || {}).map(k => ({ id: k, name: k }))
    // Fallback : mappe {id:int, name:string} → {id:name, name:name}. La UI
    // (bind:value, options) travaille désormais entièrement par NAME depuis le
    // pivot Elysium ; les vieux ids INT du fallback statique casseraient la
    // sélection auto (postQuality est un string).
    const staticToKeyed = arr => (arr || []).map(o => ({ id: o.name, name: o.name }))
    try {
      const meta = await GetElysiumMeta()
      qualityOptions = mapToOpts(meta?.qualities)
      langOptions    = mapToOpts(meta?.languages)
      subOptions     = mapToOpts(meta?.subtitles)
      if (!qualityOptions.length) qualityOptions = staticToKeyed(HYD_QUALITIES)
      if (!langOptions.length)    langOptions    = staticToKeyed(HYD_LANGUAGES)
      if (!subOptions.length)     subOptions     = staticToKeyed(HYD_SUBS)
    } catch(e) {
      console.error('GetElysiumMeta:', e)
      qualityOptions = staticToKeyed(HYD_QUALITIES)
      langOptions    = staticToKeyed(HYD_LANGUAGES)
      subOptions     = staticToKeyed(HYD_SUBS)
      addLog('META', `Elysium meta KO (${e?.message || e}) — fallback statique`)
    }
    addLog('META', `qualités: ${qualityOptions.length} · langues: ${langOptions.length} · sous-titres: ${subOptions.length}`)
    EventsOn('nzb:status',  s  => { nzbStatus = s })
    // Feedback direct du workflow Elysium (retiré : plus de cross-post — on
    // reçoit maintenant nzb:status / ddl:log directement).
    EventsOn('elysium:log', msg => addLog('ELYSIUM', msg))
    EventsOn('elysium:status', s => {
      elysiumStatus = s
      if (s?.status === 'ok' || s?.status === 'error' || s?.status === 'skip') {
        setTimeout(() => { if (elysiumStatus === s) elysiumStatus = null }, 8000)
      }
    })
    EventsOn('nzb:parpar',  p  => { if (p.percent !== undefined) nzbParparPct = p.percent })
    EventsOn('nzb:nyuu',    p  => {
      if (p.percent   !== undefined) nzbNyuuPct      = p.percent
      if (p.speed     !== undefined) nzbNyuuSpeed    = p.speed
      if (p.eta       !== undefined) nzbNyuuETA      = p.eta
      if (p.articles  !== undefined) nzbNyuuArticles = p.articles
    })
    EventsOn('ddl:progress', p => {
      if (!p.host) return
      ddlHosts = { ...ddlHosts, [p.host]: { ...ddlHosts[p.host], active: true, filename: p.filename || ddlHosts[p.host]?.filename || '', pct: p.percent ?? 0, speed: p.speed || '' } }
    })
    EventsOn('ddl:done', p => {
      if (!p.host) return
      ddlHosts = { ...ddlHosts, [p.host]: { ...ddlHosts[p.host], done: !p.error, error: p.error || '', active: false, pct: p.error ? ddlHosts[p.host]?.pct : 100 } }
    })
    EventsOn('ddl:host-skipped', host => {
      if (!host) return
      ddlHosts = { ...ddlHosts, [host]: { ...ddlHosts[host], skipped: true, active: false, error: 'skippé par utilisateur' } }
      addLog('DDL', `⏭ ${host} skippé`)
    })
    EventsOn('ddl:posting', p => {
      if (!p.host) return
      const id = p.id || 0
      ddlHosts = { ...ddlHosts, [p.host]: {
        ...ddlHosts[p.host],
        posting: !!p.posting,
        posted: p.posted ? true : ddlHosts[p.host]?.posted,
        hydrackerID: id || ddlHosts[p.host]?.hydrackerID
      } }
    })
    OnFileDrop((_x, _y, paths) => {
      addLog('QUEUE', `OnFileDrop : ${paths?.length || 0} chemin(s) reçu(s)`)
      if (!paths?.length) return
      const valid = paths.filter(p => /\.(mkv|mp4)$/i.test(p))
      addLog('QUEUE', `${valid.length} valide(s) (mkv/mp4)`)
      if (valid.length === 0) return
      // TOUJOURS enqueueOnly d'abord (garantit sort numérique), puis charge
      // la preview sur queue[0] APRÈS un debounce de 250ms. macOS peut
      // dispatcher un drop de N fichiers en N events séparés dans un ordre
      // arbitraire — sans debounce, la preview finit sur le mauvais épisode.
      valid.forEach(p => enqueueOnly(p))
      addLog('QUEUE', `DEBUG drop: valid=${valid.length}, queue.length=${queue.length}, queue[0]=${queue[0]?.split('/').pop().slice(0,40) || '?'}`)
      clearTimeout(dropDebounce)
      dropDebounce = setTimeout(() => {
        const first = queue[0]?.split('/').pop().slice(0,40) || '?'
        addLog('QUEUE', `DEBUG debounce fire: queue.length=${queue.length}, queue[0]=${first}, queueProcessing=${queueProcessing}`)
        if (queue.length >= 1 && !queueProcessing) {
          loadFileFromPath(queue[0], null)
        }
      }, 500)
    }, true)
    window.addEventListener('watch:newfile', onWatchNewFile)
    window.addEventListener('keydown', onKeydown)
  })

  // Raccourcis clavier globaux (hors champs texte)
  function onKeydown(e) {
    const tag = (e.target?.tagName || '').toLowerCase()
    const inField = tag === 'input' || tag === 'textarea' || tag === 'select'
    const mod = e.metaKey || e.ctrlKey

    // Cmd+K : focus recherche Hydracker (même depuis un input)
    if (mod && e.key === 'k') {
      e.preventDefault()
      const el = /** @type {HTMLElement|null} */ (document.querySelector('.hyd-search-input'))
      el?.focus()
      return
    }
    // Esc : stop si post en cours
    if (e.key === 'Escape' && (postLoading || queueProcessing)) {
      e.preventDefault()
      stopPost()
      return
    }
    if (inField) return  // les raccourcis suivants ne s'appliquent pas dans un champ
    // Cmd+Entrée : Lancer
    if (mod && e.key === 'Enter') {
      e.preventDefault()
      if (queue.length > 0) processQueue()
      else if (selectedHydracker && postQuality && postLanguages.length) lancerPost()
      return
    }
    // Cmd+. : Stop
    if (mod && e.key === '.') {
      e.preventDefault()
      if (postLoading || queueProcessing) stopPost()
      return
    }
    // Cmd+Backspace : Reset
    if (mod && e.key === 'Backspace') {
      e.preventDefault()
      const btn = /** @type {HTMLElement|null} */ (document.querySelector('.btn-reset'))
      btn?.click()
      return
    }
  }

  function onWatchNewFile(ev) {
    const path = ev?.detail
    if (!path) return
    enqueue(path)
  }

  // --- Queue batch ---
  let queue = []                // chemins en attente
  let queueProcessing = false
  let queueCurrent = ''
  let queueTotal = 0
  let queueDone = 0
  let queueResults = []         // [{ok, filename, message}] cumulés sur la queue
  let queueOkOpen = false       // réussites repliées par défaut

  // Traduit une erreur brute (422 Laravel, timeout…) en une phrase utile.
  // Le message complet reste consultable sous « détail ».
  function resumeErreur(msg) {
    const m = String(msg || '')
    let r
    if ((r = /The selected (language|subtitles)\.(\d+) is invalid/.exec(m))) {
      const quoi = r[1] === 'language' ? 'La langue' : 'Le sous-titre'
      return `${quoi} n°${+r[2] + 1} ne fait pas partie de la liste Elysium`
    }
    if (/The selected quality is invalid/.test(m)) return 'Qualité absente de la liste Elysium'
    if (/timeout chargement fiche/i.test(m)) return 'Fiche non chargée — délai dépassé'
    if (/timeout|deadline exceeded/i.test(m)) return 'Délai dépassé'
    if (/too many connections|482/.test(m)) return 'Toutes les connexions Usenet étaient occupées'
    if (/HTTP (\d{3})/.test(m)) return 'Elysium a refusé l’envoi (HTTP ' + /HTTP (\d{3})/.exec(m)[1] + ')'
    return m.split('\n')[0].slice(0, 120)
  }

  // Insert + sort sans toucher la preview (utilisé en batch via forEach)
  function enqueueOnly(path) {
    queue = [...queue, path].sort((a, b) =>
      a.split('/').pop().localeCompare(b.split('/').pop(), undefined, { numeric: true })
    )
    queueTotal = queueDone + queue.length + (queueProcessing ? 1 : 0)
    addLog('QUEUE', `+ ${path.split('/').pop()} (${queue.length} en attente) — clique sur ▶ Lancer pour démarrer`)
  }
  function enqueue(path) {
    enqueueOnly(path)
    // Watch folder + drop OS peuvent envoyer les fichiers UN PAR UN dans un
    // ordre arbitraire. On debounce le loadFileFromPath pour ne le lancer
    // qu'une fois queue stable et triée. Sans ça, mkvFilePath restait sur le
    // 1er fichier vu (ex : 010) au lieu du plus petit numériquement (001).
    clearTimeout(dropDebounce)
    dropDebounce = setTimeout(() => {
      if (queue.length >= 1 && !queueProcessing) {
        loadFileFromPath(queue[0], null)
      }
    }, 500)
  }
  function dequeueAt(idx) {
    queue = queue.filter((_, i) => i !== idx)
    queueTotal = queueDone + queue.length + (queueProcessing ? 1 : 0)
  }
  function clearQueue() {
    queue = []
    queueTotal = queueDone + (queueProcessing ? 1 : 0)
  }

  let queueTMDBHint = 0  // id TMDB à réutiliser pour les items suivants d'une même queue
  let queueTMDBHintType = '' // 'tv' ou 'movie' — survit au reset selectedTMDB fait par loadFileFromPath
  // Paire {wrongTmdbId → correctHydrackerId} : si l'auto-detect TMDB d'un futur
  // épisode donne le même MAUVAIS tmdb_id, on bascule direct sur la fiche corrigée.
  // Évite que le hint contamine des épisodes d'autres shows.
  let queueHydrackerHint = { wrongTmdbId: 0, correctHydrackerId: 0 }

  // Toggle Film/Série auto-pré-sélectionné selon la présence saison/épisode
  // (sinon par défaut on suppose Série puisque c'est le cas qui pose problème).
  $: hydrackerManualType = (postSeason > 0 || postEpisode > 0) ? 'tv' : (selectedTMDB?.media_type === 'movie' ? 'movie' : hydrackerManualType)

  async function processQueue() {
    if (queueProcessing) return
    // Inclut le fichier de preview dans la queue pour qu'il soit processé
    // (cas où l'user drop E1 seul d'abord, puis E2/E3 ensuite — sinon E1 skip)
    if (mkvFilePath && !queue.includes(mkvFilePath)) {
      queue = [...queue, mkvFilePath].sort((a, b) =>
        a.split('/').pop().localeCompare(b.split('/').pop(), undefined, { numeric: true })
      )
    }
    if (queue.length === 0) return
    queueCancelled = false  // nouvelle exécution de queue — on lève le flag
    queueProcessing = true
    queueResults = []
    // Snapshot de la fiche TMDB déjà sélectionnée (typiquement le show TV pour
    // les 10 épisodes droppés) → réutilisée sur tous les items suivants.
    queueTMDBHint = selectedTMDB?.id || 0
    queueTMDBHintType = selectedTMDB?.media_type || ''
    // Reset du hint Hydracker au début de chaque queue (évite de propager
    // une correction d'une queue précédente).
    queueHydrackerHint = { wrongTmdbId: 0, correctHydrackerId: 0 }
    // Pas de lock qualité/langues/subs : on laisse l'auto-detect tourner par
    // fichier, chaque épisode peut avoir des pistes ou une qualité différente.
    while (queue.length > 0) {
      const path = queue.shift()
      queue = queue
      queueCurrent = path
      const fname = path.split('/').pop()
      addLog('QUEUE', `▶ ${fname}`)
      try {
        // Si ce fichier est déjà chargé en preview avec fiches sélectionnées,
        // on réutilise tel quel — évite de reset selectedTMDB/selectedHydracker
        // et de reproposer le choix TMDB à l'utilisateur.
        const alreadyReady = mkvFilePath === path && selectedTMDB && selectedHydracker && postQuality && postLanguages.length
        if (!alreadyReady) {
          loadFileFromPath(path, null)
          // 180 s et pas 60 : en secours du raccourci Elysium, une recherche
          // proxytmdb à froid peut prendre jusqu'à 45 s (MediaSearch puis
          // TMDBSearch), ce qui dépassait l'ancien délai et faisait échouer
          // l'épisode avec « timeout chargement fiche ».
          await waitForReady(180000)
        }
        await lancerPost()
        queueDone++
        // On mémorise les canaux DEMANDÉS : sans ça le bilan de fin ne peut
        // pas distinguer « NZB manquant » de « NZB jamais demandé ».
        const voulu = { nzb: postUploadTypes.nzb, ddl: postUploadTypes.ddl }
        if (postResult) queueResults = [...queueResults,
          { ok: postResult.ok, filename: fname, message: postResult.message, voulu, path }]
      } catch(e) {
        addLog('QUEUE', `✗ ${fname} : ${e}`)
        queueResults = [...queueResults,
          { ok: false, filename: fname, message: String(e), path,
            voulu: { nzb: postUploadTypes.nzb, ddl: postUploadTypes.ddl } }]
      }
    }
    queueCurrent = ''
    queueProcessing = false
    queueTMDBHint = 0
    queueTMDBHintType = ''
    queueHydrackerHint = { wrongTmdbId: 0, correctHydrackerId: 0 }
    queueQualityHint = ''
    queueLangHint = null
    queueSubHint = null
    queueEpisodeAnchor = null
    // Récap final cumulé
    if (queueResults.length > 1) {
      const okCount = queueResults.filter(r => r.ok).length
      const koCount = queueResults.length - okCount
      // Plus de pavé de texte : le détail est rendu en liste depuis
      // queueResults, échecs en tête. Retrouver 2 erreurs dans 50 lignes
      // collées était impraticable.
      postResult = {
        ok: koCount === 0,
        message: `Queue terminée — ${okCount}/${queueResults.length} OK${koCount > 0 ? ` · ${koCount} erreur${koCount > 1 ? 's' : ''}` : ''}`,
        queue: true,
      }
      try { Notify('Go Post Tools — Queue terminée', `${okCount}/${queueResults.length} OK${koCount > 0 ? ` · ${koCount} erreur${koCount > 1 ? 's' : ''}` : ''}`) } catch(e) {}
      // Confrontation au catalogue Elysium : c'est lui qui fait foi,
      // un post peut se déclarer réussi sans que le NZB atterrisse.
      verifierSession()
    } else if (queueResults.length === 1) {
      const r = queueResults[0]
      try { Notify(r.ok ? '✓ Post terminé' : '✗ Post échoué', r.filename) } catch(e) {}
    }
  }

  function waitForReady(timeoutMs) {
    return new Promise((resolve, reject) => {
      const start = Date.now()
      const tick = () => {
        if (queueCancelled) return reject(new Error('queue annulée'))
        if (selectedHydracker && postQuality && postLanguages.length && mkvFilePath && subsAutoFilled) return resolve()
        // Si action user requise (ambiguïté TMDB ou fiche Hydracker manquante) : pause infinie
        const userActionNeeded = tmdbAmbiguous || hydrackerNotFound
        if (!userActionNeeded && Date.now() - start > timeoutMs) return reject(new Error('timeout chargement fiche'))
        setTimeout(tick, 250)
      }
      tick()
    })
  }
  let queueCancelled = false

  onDestroy(() => {
    if (debitTimer) { clearInterval(debitTimer); debitTimer = null }
    EventsOff('nzb:status', 'nzb:parpar', 'nzb:nyuu', 'ddl:progress', 'ddl:done', 'ddl:posting')
    OnFileDropOff()
    window.removeEventListener('watch:newfile', onWatchNewFile)
    window.removeEventListener('keydown', onKeydown)
  })

  // Table ISO 639-1/2 + tags de release → nom Hydracker
  const LANG_MAP = {
    // Français
    'fre':'TrueFrench','fra':'TrueFrench','fr':'TrueFrench',
    'fr-fr':'TrueFrench','fr_fr':'TrueFrench','fre-fr':'TrueFrench',
    'french':'TrueFrench','truefrench':'TrueFrench','vf':'TrueFrench','vff':'TrueFrench','vof':'TrueFrench',
    'french (canada)':'French (Canada)',
    'fr-ca':'French (Canada)','fr_ca':'French (Canada)','fre-ca':'French (Canada)',
    // VO / VO sous-titré
    'vo':'VO','voa':'VO','vost':'VO','vostfr':'VO',
    // Anglais
    'eng':'English','en':'English','english':'English',
    // Allemand
    'ger':'German','deu':'German','de':'German','german':'German',
    // Espagnol
    'spa':'Spanish','es':'Spanish','spanish':'Spanish',
    // Italien
    'ita':'Italian','it':'Italian','italian':'Italian',
    // Japonais
    'jpn':'Japanese','ja':'Japanese','japanese':'Japanese',
    // Chinois
    'chi':'Chinese','zho':'Chinese','zh':'Chinese','chinese':'Chinese',
    // Portugais
    'por':'Portuguese','pt':'Portuguese','portuguese':'Portuguese',
    // Russe
    'rus':'Russian','ru':'Russian','russian':'Russian',
    // Coréen
    'kor':'Korean','ko':'Korean','korean':'Korean',
    // Arabe
    'ara':'Arab','ar':'Arab','arab':'Arab','arabic':'Arab',
    // Néerlandais
    'nld':'Dutch','dut':'Dutch','nl':'Dutch','dutch':'Dutch',
    // Polonais
    'pol':'Polish','pl':'Polish','polish':'Polish',
    // Turc
    'tur':'Turkish','tr':'Turkish','turkish':'Turkish',
    // Suédois
    'swe':'Swedish','sv':'Swedish','swedish':'Swedish',
    // Norvégien
    'nor':'Norwegian','no':'Norwegian','norwegian':'Norwegian',
    // Danois
    'dan':'Danish','da':'Danish','danish':'Danish',
    // Finnois
    'fin':'Finnish','fi':'Finnish','finnish':'Finnish',
    // Hongrois
    'hun':'Hungarian','hu':'Hungarian','hungarian':'Hungarian',
    // Tchèque
    'cze':'Czech','ces':'Czech','cs':'Czech','czech':'Czech',
    // Roumain
    'rum':'Romanian','ron':'Romanian','ro':'Romanian','romanian':'Romanian',
    // Grec
    'gre':'Greek','ell':'Greek','el':'Greek','greek':'Greek',
    // Hébreu
    'heb':'Hebrew','he':'Hebrew','hebrew':'Hebrew',
    // Hindi
    'hin':'Hindi','hi':'Hindi','hindi':'Hindi',
    // Thaï
    'tha':'Thai','th':'Thai','thai':'Thai',
    // Ukrainien
    'ukr':'Ukrainian','uk':'Ukrainian','ukrainian':'Ukrainian',
    // Bulgare
    'bul':'Bulgarian','bg':'Bulgarian','bulgarian':'Bulgarian',
    // Croate
    'hrv':'Croatian','hr':'Croatian','croatian':'Croatian',
    // Serbe
    'srp':'Serbian','sr':'Serbian','serbian':'Serbian',
    // Slovaque
    'slk':'Slovak','sk':'Slovak','slovak':'Slovak',
    // Slovène
    'slv':'Slovenian','sl':'Slovenian','slovenian':'Slovenian',
    // Albanais
    'alb':'Albanian','sqi':'Albanian','sq':'Albanian','albanian':'Albanian',
    // Lituanien
    'lit':'Lithuanian','lt':'Lithuanian','lithuanian':'Lithuanian',
    // Letton
    'lav':'Latvian','lv':'Latvian','latvian':'Latvian',
    // Estonien
    'est':'Estonian','et':'Estonian','estonian':'Estonian',
    // Persan
    'per':'Persian','fas':'Persian','fa':'Persian','persian':'Persian',
    // Géorgien
    'geo':'Georgian','kat':'Georgian','ka':'Georgian','georgian':'Georgian',
    // Islandais
    'ice':'Icelandic','isl':'Icelandic','is':'Icelandic','icelandic':'Icelandic',
    // Mongol
    'mon':'Mongolian','mn':'Mongolian','mongolian':'Mongolian',
    // Kazakh
    'kaz':'Kazakh','kk':'Kazakh','kazakh':'Kazakh',
    // Muet
    'mue':'Muet','muet':'Muet',
  }

  function matchLang(raw) {
    const key = (raw || '').toLowerCase().trim()
    const baseKey = key.split(/[-_]/)[0]
    const mapped = LANG_MAP[key] || LANG_MAP[baseKey] || raw
    const target = mapped.toLowerCase()
    return langOptions.find(o => o.name.toLowerCase() === target)
      || langOptions.find(o => o.name.toLowerCase().includes(target) || target.includes(o.name.toLowerCase()))
      || { id: 0, name: mapped }
  }

  // Pour les subs : mappe un code/tag de piste vers une entrée de subOptions.
  // Stratégie : essai direct d'abord ('fr-ca' → 'French (Canada)' qui existe
  // dans /meta/langs), puis fallback "dégraissé" (TrueFrench → French, etc.)
  // si le nom direct n'est pas trouvé dans la liste des subs.
  function matchSub(raw) {
    const key = (raw || '').toLowerCase().trim()
    const baseKey = key.split(/[-_]/)[0]
    const mapped = LANG_MAP[key] || LANG_MAP[baseKey] || raw

    // Essai #1 : le nom mappé directement
    const direct = subOptions.find(o => o.name.toLowerCase() === mapped.toLowerCase())
    if (direct) return direct

    // Essai #2 : version "pure" pour les noms audio-only si absents côté subs
    const audioToPure = {
      'TrueFrench': 'French',
      'French (Canada)': 'French',
      'VO': 'English',
      'Muet': null,
    }
    if (mapped in audioToPure) {
      const pure = audioToPure[mapped]
      if (!pure) return { id: 0, name: mapped }
      const found = subOptions.find(o => o.name.toLowerCase() === pure.toLowerCase())
      if (found) return found
      return { id: 0, name: pure }
    }

    return { id: 0, name: mapped }
  }
  function dedupeById(arr) {
    const seen = new Set()
    return arr.filter(x => { const k = x.id !== 0 ? x.id : x.name; if (seen.has(k)) return false; seen.add(k); return true })
  }

  // Variables pour les selects d'ajout
  let langSelectValue = null
  let subSelectValue = null

  // Auto-remplissage réactif dès que API + fichier prêts.
  // Guardé par queueQualityHint : si l'user a corrigé la qualité sur EP1
  // d'une série, on ne re-détecte plus sur les épisodes suivants (elle est
  // restaurée depuis le hint dans loadFileFromPath).
  $: if (!queueQualityHint && qualityOptions.length && file?.name && !postQuality) {
    const name = file.name.toLowerCase()
    const bitrate = parseInt(String(mediaInfo?.bitrate || '').replace(/[^0-9]/g, '')) || 0
    const isH265 = /\b(x265|h\.?265|hevc)\b/i.test(file.name)
    // Cherche un quality par mots-clés présents dans le nom (insensible à la casse/espaces)
    const findQual = (...kw) => {
      const lc = kw.map(k => k.toLowerCase().replace(/[\s-]/g, ''))
      const o = qualityOptions.find(o => {
        const n = o.name.toLowerCase().replace(/[\s-]/g, '')
        return lc.every(k => n.includes(k))
      })
      return o?.id || 0
    }
    let qualID = 0
    const is4KLight = /4klight/i.test(file.name)
    const is2160p = /\b2160p\b/i.test(file.name)
    const has1080pHDLight = /1080p[\s._-]*hdlight|hdlight[\s._-]*1080p/i.test(file.name)

    // Fallbacks : après le pivot Elysium, qualityOptions.id = name (string),
    // donc les anciens fallbacks numériques Hydracker (60, 50, 94, 4, 53) ne
    // matchent plus rien. On les remplace par les noms Elysium équivalents.
    const FB = {
      ULTRA_HDLIGHT_X265: 'Ultra HDLight (x265)',
      HDLIGHT_1080P:      'HDLight 1080p',
      WEB_1080P_LIGHT:    'WEB 1080p Light',
      WEB:                'WEB',
      ULTRA_HD_X265:      'ULTRA HD (x265)',
    }
    // Règles personnalisées (ordre de priorité)
    if (/-xander(\.(mkv|mp4))?$/i.test(file.name)) {
      // -XANDER → toujours ULTRA HDLight x265
      qualID = findQual('ultra', 'hdlight', 'x265') || FB.ULTRA_HDLIGHT_X265
    } else if (is4KLight || (is2160p && bitrate > 0 && bitrate < 8000)) {
      // 4KLight OU (2160p + bitrate<8000) → ULTRA HDLight
      qualID = findQual('ultra', 'hdlight', 'x265') || FB.ULTRA_HDLIGHT_X265
    } else if (has1080pHDLight && isH265) {
      // 1080p.HDLight + H265 → HDLight 1080p x265 systématique
      qualID = findQual('hdlight', '1080p', 'x265') || findQual('hdlight', 'x265') || FB.HDLIGHT_1080P
    } else if (is2160p && isH265 && !/\bremux\b/i.test(name)) {
      // 2160p + H265 non-REMUX → ULTRA HD (x265). Recherche stricte pour
      // éviter de retomber sur Ultra HDLight (x265).
      const o = qualityOptions.find(q => {
        const n = q.name.toLowerCase().replace(/[\s-]/g, '')
        return n.includes('ultrahd') && n.includes('x265') && !n.includes('light')
      })
      qualID = o?.id || FB.ULTRA_HD_X265
    } else if (/\bweb([-.]?(?:rip|dl))?\b/.test(name) || name.includes('.web.')) {
      // WEB 1080p prioritaire si résolution présente (films ET séries)
      const is1080p = /\b1080p\b/i.test(file.name)
      const isLight = !isH265 && bitrate > 0 && bitrate <= 3000
      if (isLight) {
        qualID = findQual('web', '1080p', 'light') || findQual('webrip', '1080p', 'light') || FB.WEB_1080P_LIGHT
      } else {
        if (isH265) qualID = findQual('web', '1080p', 'x265') || findQual('webrip', '1080p', 'x265') || findQual('web', 'x265') || findQual('webrip', 'x265')
        if (!qualID && is1080p) qualID = findQual('web', '1080p') || findQual('webrip', '1080p')
        if (!qualID) qualID = FB.WEB
      }
    } else if (/\bremux\b/i.test(name)) {
      // REMUX : rip BluRay intouché (x264 généralement, full bitrate). Doit
      // être testé AVANT la règle BluRay (sinon le mot "BluRay" du filename
      // match d'abord et REMUX retombe en BluRay simple si pas de x265).
      if (is2160p) qualID = findQual('remux', '2160p') || findQual('remux', '4k')
      if (!qualID) qualID = findQual('bluray', 'remux', '1080p') || findQual('remux', '1080p') || findQual('bluray', 'remux') || findQual('remux')
    } else if (/\bblu-?ray\b/.test(name)) {
      if (isH265) qualID = findQual('bluray', 'x265')
      if (!qualID && bitrate > 0 && bitrate <= 3000) qualID = FB.HDLIGHT_1080P
    } else if (/hdlight/i.test(name)) {
      if (isH265) qualID = findQual('hdlight', 'x265')
      if (!qualID) qualID = FB.HDLIGHT_1080P
    }

    if (qualID) {
      postQuality = qualID
      qualityAutoFilled = true
      // Sanity check : l'option auto-choisie doit exister dans le dropdown,
      // sinon le select apparaît vide. Log pour diagnostic.
      const known = qualityOptions.some(o => o.id === qualID || o.name === qualID)
      if (!known) addLog('AUTO', `⚠ qualité auto "${qualID}" absente de qualityOptions (${qualityOptions.length} entrées)`)
    } else if (fileInfo?.quality) {
      // Fallback : détection classique par le parser
      const qual = fileInfo.quality.toLowerCase()
      const src  = (fileInfo.source || '').toLowerCase()
      let q = src ? qualityOptions.find(o => { const n = o.name.toLowerCase(); return n.includes(qual) && n.includes(src) }) : null
      if (!q) q = qualityOptions.find(o => o.name.toLowerCase().includes(qual))
      if (q) { postQuality = q.id; qualityAutoFilled = true }
    }
  }

  // Override : MediaInfo arrive APRÈS la première détection (qui se fait sur le
  // nom du fichier, sans bitrate). Si WEB 1080p NON-x265 + bitrate ≤ 3000 kbps,
  // bascule sur WEB 1080p Light. Les x265 restent x265 (leur bitrate bas est
  // normal pour ce codec).
  $: if (!queueQualityHint && qualityOptions.length && file?.name && mediaInfo?.bitrate && qualityAutoFilled && postQuality) {
    const bitrate = parseInt(String(mediaInfo.bitrate).replace(/[^0-9]/g, '')) || 0
    const name = file.name.toLowerCase()
    const isWeb = /\bweb([-.]?(?:rip|dl))?\b/.test(name) || name.includes('.web.')
    const isH265 = /\b(x265|h\.?265|hevc)\b/i.test(file.name)
    if (isWeb && !isH265 && bitrate > 0 && bitrate <= 3000) {
      const currentName = (qualityOptions.find(q => q.id === postQuality)?.name || '').toLowerCase()
      if (!currentName.includes('light')) {
        const findQual = (...kw) => {
          const lc = kw.map(k => k.toLowerCase().replace(/[\s-]/g, ''))
          return qualityOptions.find(o => {
            const n = o.name.toLowerCase().replace(/[\s-]/g, '')
            return lc.every(k => n.includes(k))
          })?.id || 0
        }
        const lightID = findQual('web', '1080p', 'light') || findQual('webrip', '1080p', 'light') || 94
        if (lightID && lightID !== postQuality) {
          postQuality = lightID
        }
      }
    }
  }

  // Auto-détection des langues audio : mediaInfo est prioritaire (plus fiable que le parser)
  // Si le parser a détecté "MULTi" / "MULTI" / "DUAL" on l'ignore (c'est un tag, pas une langue)
  $: if (langOptions.length && !langsAutoFilled) {
    const tracks = mediaInfo?.audios || []
    let primaryDone = false
    if (tracks.length) {
      // Nouveau : mapping par Title (FR AD, FR VFi, ENG VO…) — plus précis que par position
      const matched = tracks.map(t => mapAudioTrackByTitle(t)).filter(Boolean)
      if (matched.length) {
        postLanguages = dedupeById(matched)
        addLog('LANG', `pistes audio : ${tracks.map((t, i) => `#${i+1} "${t.title || t.language || '?'}" → ${matched[i]?.name || '?'}`).join(' · ')}`)
        primaryDone = true
      }
    } else if (mediaInfo && fileInfo?.languages?.length) {
      // Fallback parser : SEULEMENT si mediaInfo a répondu mais sans piste audio
      // exploitable. Sinon le bloc lock le flag avant que mediaInfo arrive et
      // les pistes ENG/FR détectées sont ignorées (cas folder + 1er MKV).
      const clean = fileInfo.languages.filter(l => !['multi','multil','dual','vff','vostfr','vo','vost','voa'].includes(l.toLowerCase()))
      if (clean.length) {
        postLanguages = dedupeById(clean.map(matchLang))
        primaryDone = true
      }
    }
    // Patterns spéciaux dans le nom de fichier (s'ajoutent aux langues détectées)
    const rawName = (fileInfo?.raw || file?.name || '').toUpperCase()
    if (rawName.includes('WITH.AD') || rawName.includes('WITHAD')) {
      const frad = langOptions.find(o => o.name === 'FRENCH AD')
      if (frad && !postLanguages.some(l => l && l.name === 'FRENCH AD')) {
        postLanguages = dedupeById([...postLanguages, frad])
        addLog('LANG', 'WITH.AD détecté → ajout FRENCH AD')
      }
    }
    if (rawName.includes('VOQ')) {
      const frcan = langOptions.find(o => o.name === 'French (Canada)')
      if (frcan && !postLanguages.some(l => l && l.name === 'French (Canada)')) {
        postLanguages = dedupeById([...postLanguages, frcan])
        addLog('LANG', 'VOQ détecté → ajout French (Canada)')
      }
    }
    // Ne verrouille l'auto-fill que quand mediaInfo a répondu (success OU error).
    // Sinon les hints filename verrouillent avant que les audio tracks soient analysés.
    if (primaryDone || mediaInfoError) langsAutoFilled = true
  }

  $: if (langOptions.length && subOptions.length && !subsAutoFilled) {
    // Préfère subTracks (full info) à subs (codes bruts pour rétrocompat)
    const subT = (mediaInfo?.subTracks || []).filter(t => !t.forced)
    if (subT.length) {
      const matched = subT.map(t => mapSubTrackByTitle(t)).filter(Boolean)
      addLog('SUB', `pistes : ${subT.map((t, i) => `"${t.title || t.language || '?'}" → ${matched[i]?.name || '?'}`).join(' · ')}`)
      postSubs = dedupeById(matched)
    } else if (mediaInfo?.subs?.length) {
      const rawCodes = mediaInfo.subs
      const matched = rawCodes.map(matchSub)
      addLog('SUB', `codes bruts : [${rawCodes.join(', ')}] → match : [${matched.map(s => `${s.name}${s.id ? '#'+s.id : '⚠'}`).join(', ')}]`)
      postSubs = dedupeById(matched)
    } else if (file?.name) {
      // Fallback : si MediaInfo n'a vu aucune sub (SRT externe / hardcodée),
      // on devine via filename. VOSTFR / FASTSUB / SUBFR → French.
      const rawName = (fileInfo?.raw || file.name).toUpperCase()
      if (/\b(VOSTFR|FASTSUB|SUBFR|SUB[\s._-]?FR)\b/.test(rawName)) {
        const french = subOptions.find(o => o.name === 'French')
        if (french) {
          postSubs = [french]
          addLog('SUB', '⚙ filename hint VOSTFR/FASTSUB → ajout French')
        }
      }
    }
    // Verrouille uniquement quand mediaInfo a répondu (success OU error) — pareil
    // que langs : sinon waitForReady en queue batch peut résoudre avant que les
    // subs aient été poséees par le bloc réactif.
    if (mediaInfo || mediaInfoError) subsAutoFilled = true
  }

  // Mappe une piste audio vers une option Hydracker en se basant sur le Title de
  // la piste (plus précis que la position). Title courants vus dans les MKV :
  // "FR VFi : AC3 5.1", "FR AD : AC3 5.1", "ENG VO : EAC3 5.1 ATMOS", etc.
  function mapAudioTrackByTitle(track) {
    const title = String(track?.title || '').toUpperCase()
    const lang = String(track?.language || track?.lang || '').toLowerCase()
    // Ordre important : AD doit être testé AVANT VF/VFI (sinon "FR AD VFi" matche VFi)
    if (/\b(AUDIO[\s._-]?DESCRIPTION|AUDIODESCRIPTION|\bAD\b)/i.test(title)) {
      const ad = langOptions.find(o => o.name === 'FRENCH AD')
      if (ad) return ad
    }
    if (/\b(VFQ|VOQ|QUEBEC|CANADIAN|CANADA)\b/i.test(title)) {
      const frcan = langOptions.find(o => o.name === 'French (Canada)')
      if (frcan) return frcan
    }
    if (/\b(VFI|VFF|TRUEFRENCH|TRUE[\s._-]?FRENCH)\b/i.test(title)) {
      const tf = langOptions.find(o => o.name === 'TrueFrench')
      if (tf) return tf
    }
    // Fallback : Language brut (French, English, etc.)
    if (lang) return matchLang(lang)
    return { id: 0, name: track?.title || '?' }
  }

  // Mappe une piste sub vers une option Hydracker basé sur Title + Language.
  // Hydracker n'a qu'une seule entrée "French" pour les subs (pas de SDH/Forced) —
  // donc FR Full / FR SDH mappent toutes au même "French".
  function mapSubTrackByTitle(track) {
    const lang = String(track?.language || '').toLowerCase()
    if (lang) return matchSub(lang)
    return matchSub(track?.title || '')
  }

  // [Legacy] Mappe une liste de codes ISO audio vers les noms Hydracker, en gérant les pistes multiples.
  // ex: ['fr','fr','en'] → [TrueFrench, French (Canada), English]
  //
  // Règle spéciale : si le nom du fichier contient "VFi" (VF internationale,
  // doublage français métropolitain), on ne met JAMAIS French (Canada) — c'est
  // incompatible par définition. Toutes les pistes FR → TrueFrench.
  function mapAudioTracks(codes) {
    const hasVFi = /\bvfi\b/i.test(file?.name || '')
    const frenchOrder = hasVFi
      ? ['TrueFrench', 'TrueFrench', 'FRENCH AD']
      : ['TrueFrench', 'French (Canada)', 'FRENCH AD']
    const result = []
    let frIdx = 0
    for (const code of codes) {
      const c = (code || '').toLowerCase()
      if (c.startsWith('fr')) {
        const name = frenchOrder[frIdx] || 'TrueFrench'
        frIdx++
        const found = langOptions.find(o => o.name === name)
        if (found) result.push(found)
      } else {
        result.push(matchLang(code))
      }
    }
    return result
  }

  // --- Drop zone ---
  // OnFileDrop (Wails) est le seul handler — le ondrop HTML empêche Wails de voir l'événement OS
  function onDragOver(e) { e.preventDefault(); dragOver = true }
  function onDragLeave() { dragOver = false }

  async function onFileInput(e) {
    const f = e.target.files[0]
    if (f) await loadFileFromPath(null, f.name)  // parcourir : pas de chemin OS, juste le nom
  }

  async function loadFileFromPath(path, name, opts = {}) {
    const isFolder = !!opts.isFolder
    const isArchive = !!opts.isArchive
    const filename = name || path.split(/[\\/]/).pop()
    addLog('LOAD', `→ ${filename.slice(0, 60)}`)

    // Mode Jeu : on a déjà choisi le jeu via la recherche IGDB. Le drop d'une
    // archive/ISO ne doit PAS relancer le parse + auto-detect TMDB (qui
    // écraserait la fiche jeu). On garde la sélection, on attache juste le fichier.
    if (gameMode && selectedTMDB?._igdb) {
      if (path) mkvFilePath = path
      file = { name: filename }
      addLog('IGDB', `📎 fichier attaché : ${filename}`)
      return
    }

    if (path) mkvFilePath = path
    mediaInfo = null
    selectedTMDB = null
    selectedHydracker = null
    tmdbResults = []
    hydrackerResults = []
    hydrackerSearchCache = []
    posterDataUrl = ''
    hydrackerPosterUrl = ''
    hydrackerNotFound = false
    hydrackerManualId = ''
    postQuality = ''
    postLanguages = []
    postSubs = []
    postSeason = 0
    postEpisode = 0
    postFullSaison = isFolder || isArchive  // dossier/archive = saison complète par défaut
    postNfoManual = ''
    nfoEditMode = isArchive  // archive = pas de mediaInfo → édition manuelle. Folder = on tente sur le 1er MKV.
    langsAutoFilled = false
    subsAutoFilled = false
    qualityAutoFilled = false
    // Sticky qualité série : restaure le choix manuel de l'user pour tous
    // les épisodes suivants (fixée sur EP1, héritée sur EP2, EP3…).
    if (queueQualityHint) postQuality = queueQualityHint
    // Idem langues / sous-titres. Les drapeaux *AutoFilled passent à true
    // pour neutraliser les blocs réactifs d'auto-détection : sans ça, la
    // lecture MediaInfo de l'épisode écraserait le choix de l'user dès
    // qu'elle arrive.
    if (queueLangHint) { postLanguages = [...queueLangHint]; langsAutoFilled = true }
    if (queueSubHint)  { postSubs      = [...queueSubHint];  subsAutoFilled  = true }

    // Objet file synthétique pour afficher le nom
    file = { name: filename }

    // 1. Parser le nom de fichier (ou nom du dossier pour les saisons complètes)
    fileInfo = await ParseFilename(filename)
    addLog('TMDB', `parse : title="${fileInfo?.title || ''}" year="${fileInfo?.year || ''}" S${fileInfo?.season || 0}E${fileInfo?.episode || 0}${isFolder ? ' (dossier saison)' : isArchive ? ' (archive saison)' : ''}`)
    if (fileInfo?.season) postSeason = fileInfo.season
    if (fileInfo?.episode && !isFolder && !isArchive) postEpisode = fileInfo.episode
    // Fallback anime numéroté : parser Go cherche S01E01/1x01/Saison 01 mais
    // pas « TITRE NNN ». Ex : « One Piece 010 MULTI … » → parser Go extrait
    // title="One Piece 010" (pollué). On détecte le nombre en fin de titre
    // parsé, on l'utilise comme épisode et on nettoie le titre pour que la
    // recherche TMDB matche « One Piece » et pas « One Piece 010 ».
    if (!postEpisode && !isFolder && !isArchive && fileInfo?.title) {
      const m = fileInfo.title.match(/^(.+?)[\s._-]+(\d{1,4})$/)
      if (m) {
        const n = parseInt(m[2])
        if (n > 0 && n < 1900) {
          const cleanTitle = m[1].trim()
          fileInfo = { ...fileInfo, title: cleanTitle, episode: n }
          postEpisode = n
          addLog('TMDB', `↺ anime numéroté détecté : titre="${cleanTitle}" ep=${n}`)
        }
      }
    }
    // Ancre de numérotation manuelle : si l'user a re-mappé un épisode précédent
    // de la queue (ex : anime numéroté 062 → S02E01), on applique l'offset au
    // fichier courant. Rétroactif ignoré (rawEp < anchor.rawEp).
    if (queueEpisodeAnchor && !isFolder && !isArchive) {
      const rawEp = fileInfo?.episode || 0
      if (rawEp >= queueEpisodeAnchor.rawEp) {
        postSeason = queueEpisodeAnchor.saison
        postEpisode = queueEpisodeAnchor.episode + (rawEp - queueEpisodeAnchor.rawEp)
        addLog('QUEUE', `↺ numérotation sticky : raw ${rawEp} → S${String(postSeason).padStart(2,'0')}E${String(postEpisode).padStart(2,'0')}`)
      }
    } else if (!isFolder && !isArchive && postSeason > 0 && postEpisode > 0 && (queue.length > 0 || queueProcessing)) {
      // Auto-ancre : le 1er fichier de la queue arrive avec Saison + Épisode
      // détectés (ex : « Saison 01 » + fallback anime 001). On mémorise pour
      // que les épisodes suivants sans « Saison XX » dans le nom héritent.
      queueEpisodeAnchor = { rawEp: postEpisode, saison: postSeason, episode: postEpisode }
      addLog('QUEUE', `↺ ancre auto : raw ${postEpisode} → S${String(postSeason).padStart(2,'0')}E${String(postEpisode).padStart(2,'0')}`)
    }

    // 2. MediaInfo via Go : pour un fichier direct, OU pour le 1er MKV du dossier (saison complète)
    if (path && !isArchive) {
      let miPath = path
      if (isFolder) {
        try {
          miPath = await FindFirstMkvInFolder(path)
          addLog('MI', `📂 saison : analyse du 1er épisode → ${miPath.split('/').pop()}`)
        } catch(e) {
          addLog('MI', `⚠ pas de MKV trouvé dans le dossier : ${e}`)
          miPath = ''
        }
      }
      if (miPath) await analyzeMediaInfoFromPath(miPath)
    }

    // 3. Recherche TMDB automatique
    if (fileInfo?.title) {
      await autoSearchTMDB(fileInfo.title)
    } else {
      addLog('TMDB', '⚠ pas de titre extrait du nom de fichier — recherche auto annulée')
    }
  }

  // ── Bilan de session ────────────────────────────────────────────────
  // À la fin d'une file, on confronte ce qu'on croit avoir posté à ce qui
  // est RÉELLEMENT arrivé sur Elysium. Un post peut se déclarer réussi
  // sans que le NZB atterrisse ; seul le catalogue fait foi.
  let sessionReport = null      // [{filename, path, voulu, nzb, ddl}]
  let sessionChecking = false

  // Normalise un nom pour comparer les deux côtés : Elysium préfixe les
  // releases de série par « SxxEyy », et l'extension diffère parfois.
  function _normNom(n) {
    return String(n || '')
      .replace(/\.[a-z0-9]{2,4}$/i, '')                      // extension
      .replace(/^\s*S\d{1,2}[\s._-]?E\d{1,4}\s+/i, '')     // préfixe SxxEyy
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '')
  }

  async function verifierSession() {
    if (!queueResults.length) return
    sessionChecking = true
    try {
      // 3 pages de chaque canal : large devant une file typique, et ça
      // reste 6 appels au total.
      const prendre = async (fn) => {
        const vus = []
        for (let page = 1; page <= 3; page++) {
          const r = await fn(page)
          const lot = r?.data || []
          vus.push(...lot)
          if (lot.length === 0) break
        }
        return new Set(vus.map(x => _normNom(x.file_name)))
      }
      const [nzbSet, ddlSet] = await Promise.all([
        prendre(ListMyElyNzbs).catch(() => new Set()),
        prendre(ListMyElyDdls).catch(() => new Set()),
      ])
      sessionReport = queueResults.map(r => {
        const k = _normNom(r.filename)
        return {
          filename: r.filename,
          path: r.path,
          voulu: r.voulu || { nzb: false, ddl: false },
          nzb: nzbSet.has(k),
          ddl: ddlSet.has(k),
        }
      })
      const manquants = sessionReport.filter(x => x.voulu.nzb && !x.nzb).length
      addLog('QUEUE', `📋 bilan : ${sessionReport.length} release(s) vérifiée(s) sur Elysium`
        + (manquants ? ` — ${manquants} NZB manquant(s)` : ' — tout est là'))
    } catch (e) {
      addLog('QUEUE', `⚠ bilan indisponible : ${e}`)
      sessionReport = null
    }
    sessionChecking = false
  }

  // Reconstruit une file avec les seules releases dont le NZB manque, en
  // ne réactivant QUE le canal NZB : le DDL est déjà en ligne, le reposter
  // créerait un doublon.
  function reposterNzbManquants() {
    const aReprendre = (sessionReport || []).filter(x => x.voulu.nzb && !x.nzb && x.path)
    if (!aReprendre.length) return
    postUploadTypes = { ...postUploadTypes, nzb: true, ddl: false }
    queue = aReprendre.map(x => x.path)
    sessionReport = null
    addLog('QUEUE', `↻ rattrapage : ${queue.length} NZB à reposter (DDL désactivé)`)
  }

  // --- MediaInfo via Go filesystem ---
  async function analyzeMediaInfoFromPath(path) {
    mediaInfoLoading = true
    mediaInfoError = ''
    const MediaInfo = (await import('mediainfo.js')).default
    const toU8 = (bytes) => {
      if (bytes instanceof Uint8Array) return bytes
      if (Array.isArray(bytes)) return new Uint8Array(bytes)
      if (typeof bytes === 'string') {
        const bin = atob(bytes)
        const u8 = new Uint8Array(bin.length)
        for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i)
        return u8
      }
      return new Uint8Array(0)
    }
    const readChunk = async (size, offset) => toU8(await ReadFileChunk(path, offset, size))
    const fileSize = await GetFileSize(path)
    const getSize = () => fileSize

    // Le WASM mediainfo plante parfois avec « exit(NNN) » sur des MKV avec
    // chapitres/tags atypiques — le retry avec une instance fraîche passe la
    // grande majorité des cas.
    const runOnce = async () => {
      let mi
      try {
        mi = await MediaInfo({ format: 'object', locateFile: () => '/MediaInfoModule.wasm' })
        return await mi.analyzeData(getSize, readChunk)
      } finally {
        try { mi?.close() } catch(_) {}
      }
    }

    addLog('MI', `analyse : ${path.split('/').pop()}`)
    try {
      // Ordre inversé (v9.2.3) : binaire natif embarqué EN PREMIER — instantané
      // sur les MKV LiHDL (x265/H265) qui font planter le WASM avec exit(NNN),
      // ce qui gaspillait 2-5s d'essais WASM avant de tomber sur ce même binaire.
      // WASM ne sert plus que si le binaire natif est indisponible (rare : uniquement
      // si binutil.ExtractBinary rate ET pas de mediainfo dans PATH).
      let result
      try {
        const native = await MediaInfoNative(path)
        result = JSON.parse(native)
      } catch(eNative) {
        const msgN = String(eNative?.message || eNative || 'erreur inconnue')
        addLog('MI', `⚠ binaire natif KO (${msgN}) — fallback WASM`)
        try {
          result = await runOnce()
        } catch(e1) {
          const msg1 = String(e1?.message || e1 || 'erreur inconnue')
          addLog('MI', `⚠ WASM KO (${msg1}) — retry`)
          result = await runOnce()
        }
      }
      mediaInfo = parseMediaInfo(result)
      if (!mediaInfo || (!mediaInfo.filesize && !mediaInfo.videoCodec && !mediaInfo.duration)) {
        mediaInfoError = 'Aucune donnée exploitable'
        mediaInfo = null
        addLog('MI', `⚠ ${mediaInfoError}`)
      } else {
        addLog('MI', `✓ ${mediaInfo.videoCodec || '?'} · ${mediaInfo.audios?.length || 0} audio · ${mediaInfo.subs?.length || 0} subs`)
      }
    } catch(e) {
      const msg = String(e?.message || e || 'erreur inconnue')
      mediaInfoError = msg
      addLog('MI', `✗ erreur (2 essais) : ${msg}`)
    }
    mediaInfoLoading = false
  }

  // Extrait un code langue depuis le Title d'une piste MediaInfo (ex: "ENG VO" → "eng").
  // Ignore les tags VO/VOSTFR qui sont des indicateurs de sous-titrage, pas des langues.
  function extractLangFromTitle(title) {
    if (!title) return null
    const t = String(title).toUpperCase()
    const m = t.match(/\b(ENGLISH|ENG|FRENCH|TRUEFRENCH|FRA|FRE|GERMAN|DEU|GER|SPANISH|SPA|ITALIAN|ITA|JAPANESE|JPN|JAP|RUSSIAN|RUS|CHINESE|CHI|KOREAN|KOR|PORTUGUESE|POR|DUTCH|NLD|DUT|POLISH|POL|ARABIC|ARA|TURKISH|TUR)\b/)
    return m ? m[1].toLowerCase() : null
  }

  function parseMediaInfo(result) {
    const tracks = result?.media?.track || []
    const typesFound = tracks.map(t => t['@type'] || '?').join(', ')
    addLog('MI', `pistes : [${typesFound}]`)
    const general = tracks.find(t => t['@type'] === 'General') || {}
    const video = tracks.find(t => t['@type'] === 'Video') || {}
    const audios = tracks.filter(t => t['@type'] === 'Audio')
    // Plus permissif : Text ou Subtitle (selon les variantes mediainfo.js)
    const texts = tracks.filter(t => {
      const ty = String(t['@type'] || '').toLowerCase()
      return ty === 'text' || ty === 'subtitle'
    })

    return {
      duration: general.Duration ? formatDuration(parseFloat(general.Duration)) : null,
      filesize: general.FileSize ? formatSize(parseInt(general.FileSize)) : null,
      // Taille brute en octets : le panneau de suivi calcule un volume
      // transféré, ce que la chaîne formatée ne permet pas.
      filesizeBytes: general.FileSize ? parseInt(general.FileSize) : 0,
      videoCodec: video.Format || video.CodecID || null,
      videoProfile: video.Format_Profile || null,
      width: video.Width || null,
      height: video.Height || null,
      bitrate: video.BitRate ? Math.round(parseInt(video.BitRate) / 1000) + ' kbps' : null,
      audios: audios.map(a => ({
        codec: a.Format || a.CodecID || '?',
        channels: a.Channels ? a.Channels + 'ch' : null,
        lang: a.Language || extractLangFromTitle(a.Title) || null,
        title: a.Title || null,
        language: a.Language || null,
      })),
      langs: [...new Set(audios.map(a => a.Language || extractLangFromTitle(a.Title)).filter(Boolean))],
      // Toutes les pistes texte avec leurs métadonnées (Title + Language + Forced).
      // Les forced sont conservées dans subTracks pour l'affichage mais filtrées
      // de `subs` (utilisé pour l'auto-fill — on ne veut pas les forced sur la fiche).
      subTracks: texts.map(t => ({
        title: t.Title || null,
        language: t.Language || null,
        forced: String(t.Forced || '').toLowerCase() === 'yes',
      })),
      subs: texts
        .filter(t => String(t.Forced || '').toLowerCase() !== 'yes')
        .map(t => t.Language || extractLangFromTitle(t.Title) || t.Title)
        .filter(Boolean),
    }
  }

  function formatDuration(s) {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
    return h > 0 ? `${h}h${String(m).padStart(2,'0')}m` : `${m}m${String(sec).padStart(2,'0')}s`
  }
  function formatSize(b) {
    if (b > 1e9) return (b/1e9).toFixed(2) + ' GB'
    return (b/1e6).toFixed(0) + ' MB'
  }

  // --- TMDB ---
  async function autoSearchTMDB(query) {
    tmdbSearchLoading = true
    rechercheMessage = ''
    // Shortcut queue SÉRIES uniquement : on hérite la fiche TMDB du 1er
    // épisode de la queue pour les suivants (EP001→EP002→…), ce qui évite
    // le prompt manuel de recherche entre chaque épisode d'une série. NE PAS
    // appliquer aux films : une queue de films distincts (Nativity, Hard
    // Way, Bachelor Night) ne partage pas de fiche, sinon tout tombe sous
    // la fiche du 1er film. Garde : le fichier courant doit ressembler à un
    // épisode (fileInfo.episode > 0 ou pattern SxxEyy détecté).
    const currentIsEpisode = (fileInfo?.episode || 0) > 0 || /\bS\d{1,2}[\s._-]?E\d{1,4}\b/i.test(file?.name || '')
    // Le type doit venir du hint (queueTMDBHintType), PAS de selectedTMDB : ce
    // dernier vient d'être resetté à null par loadFileFromPath juste avant.
    const hintIsSeries = queueTMDBHintType === 'tv'
    if (queueTMDBHint && hintIsSeries && currentIsEpisode && (queue.length > 0 || queueProcessing)) {
      try {
        const movie = await TMDBGetByID(queueTMDBHint, 'tv')
        if (movie) {
          movie.media_type = 'tv'
          addLog('TMDB', `↺ fiche série héritée queue (tmdb #${queueTMDBHint})`)
          await selectTMDB(movie)
          tmdbSearchLoading = false
          return
        }
      } catch (_) {}
    }
    addLog('TMDB', `🔍 Recherche auto : "${query}"${fileInfo?.year ? ' (' + fileInfo.year + ')' : ''}`)

    // Raccourci catalogue Elysium pour les ÉPISODES DE SÉRIE.
    // Les releases de séries n'ont presque jamais l'année dans le nom, or sans
    // année le proxytmdb doit balayer toute sa base : mesuré 9,8 s pour
    // « Dallas S08E01 » et 19,2 s pour « True Detective S04E01 » à froid, ce
    // qui dépassait le timeout du client et ne remontait aucune fiche.
    // Elysium connaît déjà le catalogue et répond en ~0,2 s avec le tmdb_id :
    // on s'en sert pour sauter directement au lookup par id (instantané).
    // Garde : correspondance EXACTE du titre, sinon on laisse la recherche
    // normale décider plutôt que d'imposer une fiche approchante.
    if (currentIsEpisode && query) {
      const norm = s => String(s || '').toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()
      try {
        const ely = await ElysiumSearchTitles(query, 'tv') || []
        const exacts = ely.filter(t => t?.tmdb_id && norm(t.title) === norm(query))
        // ⚠ Plusieurs fiches peuvent porter EXACTEMENT le même titre : « One
        // Piece » existe en animé (tmdb 37854) et en série live (111110).
        // Prendre la première reviendrait à poster sur la mauvaise fiche sans
        // rien dire. Dans ce cas on ne tranche pas : la recherche normale
        // affichera le choix à l'utilisateur.
        if (exacts.length > 1) {
          addLog('TMDB', `⚠ ${exacts.length} fiches Elysium nommées « ${query} » `
            + `(tmdb ${exacts.map(t => t.tmdb_id).join(', ')}) — choix laissé à la recherche`)
        }
        const exact = exacts.length === 1 ? exacts[0] : null
        if (exact) {
          const movie = await TMDBGetByID(exact.tmdb_id, 'tv')
          if (movie) {
            movie.media_type = 'tv'
            addLog('TMDB', `⚡ fiche trouvée via Elysium : ${exact.title} (tmdb #${exact.tmdb_id})`)
            await selectTMDB(movie)
            tmdbSearchLoading = false
            return
          }
        }
      } catch (e) { addLog('TMDB', `⚠ raccourci Elysium indisponible (${e}) — recherche normale`) }
    }

    try {
      // Recherche via mediasearch (plus fiable, inclut le tmdb_id directement)
      // Le proxy exige année OU pattern SxxEyy — sinon "Year or episode not found".
      // ⚠ Si on a une saison/épisode, on DOIT ajouter SxxEyy (même avec year),
      // sinon le proxy cherche seulement dans les films et rate les séries dont
      // le nom contient l'année (ex : Truth.Be.Told.2019.S01E01 → query
      // « Truth Be Told 2019 » = 404 côté proxy, alors que « Truth Be Told
      // 2019 S01E01 » remonte la série).
      const year = fileInfo?.year || ''
      let q = query
      if (fileInfo?.season && fileInfo?.episode) {
        const s = String(fileInfo.season).padStart(2, '0')
        const e = String(fileInfo.episode).padStart(2, '0')
        q = year ? `${query} ${year} S${s}E${e}` : `${query} S${s}E${e}`
      } else if (fileInfo?.season) {
        const s = String(fileInfo.season).padStart(2, '0')
        q = year ? `${query} ${year} S${s}E01` : `${query} S${s}E01`
      } else if (year) {
        q = `${query} ${year}`
      }
      let spResults = []
      try { spResults = await MediaSearch(q) || [] } catch(e) { addLog('TMDB', '⚠ mediasearch: ' + e) }
      addLog('TMDB', `mediasearch : ${spResults.length} résultat(s)`)

      if (spResults.length === 0) {
        // Fallback sur TMDB direct si mediasearch ne trouve rien
        try { tmdbResults = await TMDBSearch(q) || [] } catch(e) { addLog('TMDB', '✗ TMDB API : ' + e); tmdbResults = [] }
        addLog('TMDB', `TMDB API : ${tmdbResults.length} résultat(s)`)
      } else {
        // Convertit les résultats mediasearch en objets compatibles TMDB pour la modal
        tmdbResults = spResults.map(r => ({
          id: r.tmdb_id,
          media_type: r.media_type,
          title: r.title_fr || r.title_vo,
          name: r.title_fr || r.title_vo,
          original_title: r.title_vo || '',
          original_name: r.title_vo || '',
          release_date: r.year ? r.year + '-01-01' : '',
          first_air_date: r.year ? r.year + '-01-01' : '',
          poster_path: '',
          _poster_full: r.poster_url,
          _from_mediasearch: true,
        }))
      }

      if (tmdbResults.length === 0) {
        // Ne jamais rester muet : sans ce message, l'écran reste vide et on
        // attend une fiche qui ne viendra pas (constaté sur Herbie 1977).
        rechercheMessage = `Aucune fiche trouvée pour « ${query} »${fileInfo?.year ? ' (' + fileInfo.year + ')' : ''}. Cherche à la main ou colle un ID TMDB.`
        addLog('TMDB', '⚠ ' + rechercheMessage)
      } else if (tmdbResults.length === 1) {
        // Un résultat unique n'est pas forcément le bon : pour « One Piece »
        // le proxy a renvoyé la série « Piece » (tmdb 46131), sélectionnée
        // d'office, ce qui envoyait la saison sur la mauvaise fiche. On
        // n'accepte en automatique que si le titre ressemble vraiment à ce
        // qu'on cherchait ; sinon on laisse l'utilisateur trancher.
        //
        // ⚠ Comparer AUSSI le titre original : les noms de fichiers portent le
        // titre VO, alors que le proxy renvoie le titre français. « Herbie Goes
        // To Monte Carlo » contre « La Coccinelle à Monte-Carlo » était rejeté
        // alors que c'était la bonne fiche.
        const nrm = s => String(s || '').toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()
        const seul = tmdbResults[0]
        const cible = nrm(query)
        const candidats = [seul.title, seul.name, seul.original_title, seul.original_name]
          .map(nrm).filter(Boolean)
        const proche = candidats.some(t => t === cible || t.includes(cible) || cible.includes(t))
        if (proche) {
          await selectTMDB(seul)
        } else {
          rechercheMessage = `Un seul résultat, « ${seul.title || seul.name} », trop éloigné de « ${query} » — choisis toi-même.`
          addLog('TMDB', '⚠ ' + rechercheMessage)
          tmdbAmbiguous = true
        }
      } else if (tmdbResults.length > 1) {
        // En mode queue, si on a un hint (fiche TMDB sélectionnée pour un épisode
        // précédent du même show), on l'applique automatiquement sans reposer la
        // question à l'utilisateur.
        const hinted = queueTMDBHint ? tmdbResults.find(r => r.id === queueTMDBHint) : null
        if (hinted) {
          addLog('TMDB', `↺ fiche réutilisée depuis la queue (id ${queueTMDBHint})`)
          await selectTMDB(hinted)
        } else {
          // Pré-filtre "match exact du titre parsé" : si un SEUL résultat a un titre
          // qui matche exactement le titre du fichier (normalisé), on auto-sélectionne
          // — sinon on ouvre le modal comme avant. Évite de faire cliquer l'user quand
          // c'est évident (ex : "Ted Lasso" vs "The Glass Box" / "The Class").
          const norm = s => (s || '').toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()
          const target = norm(query)
          // Titre français OU titre original : voir plus haut.
          const exact = tmdbResults.filter(r =>
            [r.title, r.name, r.original_title, r.original_name].map(norm).includes(target))
          if (exact.length === 1) {
            addLog('TMDB', `↺ match exact "${query}" — auto-sélection`)
            await selectTMDB(exact[0])
          } else {
            tmdbAmbiguous = true
          }
        }
      }
    } catch (e) {
      // Ne plus avaler l'erreur : un échec silencieux laissait l'écran vide
      // sans la moindre trace, ni à l'écran ni au Journal.
      rechercheMessage = `Recherche impossible : ${e?.message || e}`
      addLog('TMDB', '✗ ' + rechercheMessage)
      console.error(e)
    }
    tmdbSearchLoading = false
  }

  async function manualTMDBSearch() {
    tmdbSearchLoading = true
    tmdbAmbiguous = false
    rechercheMessage = ''
    try {
      // ===== Mode Jeu : recherche IGDB =====
      if (gameMode) {
        igdbResults = []
        if (tmdbSearchId) {
          const g = await IgdbGetByID(parseInt(tmdbSearchId))
          igdbResults = g ? [g] : []
        } else if (tmdbSearchQuery) {
          igdbResults = await IgdbSearch(tmdbSearchQuery) || []
        }
        addLog('IGDB', `${igdbResults.length} jeu(x) trouvé(s)`)
        if (igdbResults.length === 1) await selectGame(igdbResults[0])
        tmdbSearchLoading = false
        return
      }
      // ===== Mode Film/Série : TMDB =====
      if (tmdbSearchId) {
        const movie = await TMDBGetByID(parseInt(tmdbSearchId), tmdbSearchType)
        if (movie) movie.media_type = tmdbSearchType
        tmdbResults = movie ? [movie] : []
      } else if (tmdbSearchQuery) {
        const all = await TMDBSearch(tmdbSearchQuery) || []
        // Filtre par type sélectionné, puis ESTAMPILLE le type choisi
        // explicitement dans le sélecteur. Sans ça un résultat sans media_type
        // restait de type inconnu, et la mémorisation de la fiche pour la suite
        // de la file (qui exige media_type === 'tv') ne se déclenchait jamais :
        // l'app redemandait de choisir à CHAQUE épisode.
        tmdbResults = all
          .filter(r => !r.media_type || r.media_type === tmdbSearchType)
          .map(r => (r.media_type ? r : { ...r, media_type: tmdbSearchType }))
        addLog('TMDB', `Recherche "${tmdbSearchQuery}" (${tmdbSearchType}) : ${all.length} brut → ${tmdbResults.length} après filtre`)
      }
      if (tmdbResults.length === 1) selectTMDB(tmdbResults[0])
      else if (tmdbResults.length > 1) tmdbAmbiguous = true
    } catch(e) { addLog(gameMode ? 'IGDB' : 'TMDB', '✗ ' + e); console.error(e) }
    tmdbSearchLoading = false
  }

  // selectGame : sélectionne un jeu IGDB → cherche la fiche Hydracker par
  // igdb_id, synthétise un pseudo-selectedTMDB pour que le bloc post-options
  // s'affiche (le post se fait par title_id, codec-agnostique).
  async function selectGame(game) {
    igdbResults = []
    tmdbAmbiguous = false
    selectedHydracker = null
    hydrackerPosterUrl = ''
    // Pseudo-objet TMDB pour réutiliser tout le bloc post-options existant
    selectedTMDB = {
      id: game.id,
      title: game.name,
      name: game.name,
      media_type: 'game',
      release_date: game.Year ? game.Year + '-01-01' : '',
      _igdb: true,
    }
    posterDataUrl = ''
    if (game.CoverURL) { try { posterDataUrl = await FetchImageBase64(game.CoverURL) } catch(e) {} }
    addLog('IGDB', `▶ ${game.name} (IGDB #${game.id})`)
    // Lookup fiche Hydracker par igdb_id
    hydrackerNotFound = false
    try {
      const found = await HydrackerGetByIgdbID(game.id)
      if (found && found.id) {
        addLog('IGDB', `✓ Fiche Hydracker #${found.id} — ${found.name}`)
        await selectHydracker(found)
      } else {
        hydrackerNotFound = true
        addLog('IGDB', `⚠ Pas de fiche Hydracker pour IGDB #${game.id} — à créer`)
      }
    } catch(e) {
      hydrackerNotFound = true
      addLog('IGDB', `✗ lookup Hydracker : ${e}`)
    }
  }

  // Re-fetch TMDB depuis l'ID édité dans la card "Fiche Hydracker introuvable"
  // puis re-déclenche la chaîne selectTMDB → recherche Hydracker auto.
  async function refreshTmdbFromManual() {
    const id = parseInt(manualTmdbIdEdit)
    if (!id || id <= 0) return
    tmdbReloadLoading = true
    try {
      const mediaType = selectedTMDB?.media_type || 'movie'
      const movie = await TMDBGetByID(id, mediaType)
      if (movie) {
        movie.media_type = mediaType
        await selectTMDB(movie)
      } else {
        addLog('TMDB', `✗ ID ${id} introuvable côté TMDB (type ${mediaType})`)
      }
    } catch(e) {
      addLog('TMDB', `✗ reload ID ${id}: ${e}`)
    }
    tmdbReloadLoading = false
  }

  async function selectTMDB(movie) {
    // Si le movie vient de mediasearch, on enrichit avec TMDB pour avoir synopsis + poster_path officiel
    if (movie._from_mediasearch && movie.id) {
      try {
        const full = await TMDBGetByID(movie.id, movie.media_type || 'movie')
        if (full) { movie = { ...movie, ...full } }
      } catch(e) { console.error('enrich TMDB:', e) }
    }
    selectedTMDB = movie
    tmdbAmbiguous = false
    // Correction manuelle pendant une queue série : resynchroniser le hint TMDB
    // pour que les épisodes suivants héritent de la BONNE fiche (sinon E2 relit
    // le hint fixé au démarrage = mauvaise fiche que l'user vient de corriger).
    // On mémorise si la fiche est marquée série OU si le fichier courant est
    // manifestement un épisode : certains résultats remontent sans media_type
    // et on ne veut pas perdre le choix de l'utilisateur pour autant.
    const _estEpisode = (fileInfo?.episode || 0) > 0 || (postSeason > 0 && postEpisode > 0)
      || /\bS\d{1,2}[\s._-]?E\d{1,4}\b/i.test(file?.name || '')
    if ((queue.length > 0 || queueProcessing) && movie?.id
        && (movie?.media_type === 'tv' || _estEpisode)) {
      if (queueTMDBHint !== movie.id) {
        addLog('QUEUE', `↺ hint TMDB resync : #${queueTMDBHint || '?'} → #${movie.id} (correction manuelle)`)
        queueTMDBHint = movie.id
        queueTMDBHintType = 'tv'
      }
    }
    posterDataUrl = ''
    selectedHydracker = null
    hydrackerPosterUrl = ''
    if (movie.poster_path) {
      try {
        posterDataUrl = await FetchImageBase64('https://image.tmdb.org/t/p/w342' + movie.poster_path)
      } catch(e) { console.error('poster fetch error:', e) }
    } else if (movie._poster_full) {
      try { posterDataUrl = await FetchImageBase64(movie._poster_full) } catch(e) {}
    }
    // Auto-recherche Hydracker par ID TMDB
    hydrackerNotFound = false
    hydrackerManualId = ''
    if (movie.id) {
      try {
        // Hint Hydracker : utilisé UNIQUEMENT pour les épisodes de séries (pas
        // les films), et seulement si l'auto-detect TMDB tombe sur le même
        // mauvais tmdb_id que celui qui a déclenché la correction manuelle.
        // → Évite de contaminer une queue de films distincts.
        // Post-pivot Elysium : queueHydrackerHint est obsolète (stockait un
        // Hydracker fiche id, plus utilisable). On tape direct par tmdb_id +
        // mediaType (les 2 sont nécessaires — voir HydrackerGetByTmdbID).
        const found = await HydrackerGetByTmdbID(movie.id, movie.media_type || '')
        if (found) {
          // Si la fiche trouvée pointe vers un tmdb_id différent (ex: queue
          // batch série mal détectée → Lawn & Order au lieu de Law & Order),
          // resync le poster du haut sur le bon TMDB.
          const wantedTmdbId = found.tmdb_id || found.tmdbID
          if (wantedTmdbId && wantedTmdbId !== movie.id) {
            try {
              const mediaType = found.type === 'movie' ? 'movie' : 'tv'
              const correctMovie = await TMDBGetByID(wantedTmdbId, mediaType)
              if (correctMovie) {
                selectedTMDB = correctMovie
                posterDataUrl = ''
                if (correctMovie.poster_path) {
                  try { posterDataUrl = await FetchImageBase64('https://image.tmdb.org/t/p/w342' + correctMovie.poster_path) } catch(e) {}
                }
              }
            } catch(e) {}
          }
          await selectHydracker(found)
        } else {
          // Fallback : recherche par titre
          const title = movie.title || movie.name
          let matched = false
          if (title) {
            const results = await HydrackerSearch(title) || []
            hydrackerSearchCache = results
            if (results.length === 1) {
              await selectHydracker(results[0])
              matched = true
            } else if (results.length > 1) {
              hydrackerResults = results
              matched = true
            }
          }
          if (!matched) hydrackerNotFound = true
        }
      } catch(e) {
        console.error(e)
        hydrackerNotFound = true
      }
    }
  }

  let hydrackerManualError = ''
  async function confirmHydrackerID() {
    hydrackerManualError = ''
    const id = parseInt(hydrackerManualId)
    if (!id) {
      hydrackerManualError = 'ID invalide'
      return
    }
    hydrackerManualLoading = true
    addLog('HYD', `🔍 Lookup ID ${id}…`)
    try {
      const found = await HydrackerGetByID(id)
      if (found && found.id) {
        addLog('HYD', `✓ Fiche #${found.id} — ${found.name}`)
        hydrackerNotFound = false
        // Mémorise la paire {mauvais tmdb auto-detect → bonne fiche validée}
        // UNIQUEMENT si on est sur une série (épisode multi-batch). Les films
        // ne partagent jamais de fiche entre eux.
        const isSeriesEpisode = (selectedTMDB?.media_type === 'tv') || /\bS\d{1,2}E\d{1,3}\b/i.test(file?.name || '') || found.type === 'tv' || found.type === 'series'
        if (isSeriesEpisode) {
          queueHydrackerHint = {
            wrongTmdbId: selectedTMDB?.id || 0,
            correctHydrackerId: found.id,
          }
        }
        // Si la fiche Hydracker est liée à un autre tmdb_id que celui auto-detecté,
        // on re-fetch TMDB pour mettre à jour le poster + titre/année du haut.
        // Le mediaType vient du toggle Film/Série (l'auto-detect via found.type
        // peut être ambigu : Hydracker utilise "series"/"tv"/"movie"...).
        const mediaType = hydrackerManualType === 'movie' ? 'movie' : 'tv'
        const wantedTmdbId = found.tmdb_id || found.tmdbID
        if (wantedTmdbId && (!selectedTMDB || selectedTMDB.id !== wantedTmdbId || selectedTMDB.media_type !== mediaType)) {
          try {
            const movie = await TMDBGetByID(wantedTmdbId, mediaType)
            if (movie) {
              selectedTMDB = movie
              tmdbAmbiguous = false
              // Correction manuelle pendant une queue série : resync le hint TMDB
              // pour que les épisodes suivants héritent de la bonne fiche.
              if ((queue.length > 0 || queueProcessing) && mediaType === 'tv' && queueTMDBHint !== movie.id) {
                addLog('QUEUE', `↺ hint TMDB resync : #${queueTMDBHint || '?'} → #${movie.id} (fiche Elysium manuelle)`)
                queueTMDBHint = movie.id
                queueTMDBHintType = 'tv'
              }
              posterDataUrl = ''
              if (movie.poster_path) {
                try { posterDataUrl = await FetchImageBase64('https://image.tmdb.org/t/p/w342' + movie.poster_path) } catch(e) {}
              }
              addLog('HYD', `↻ TMDB resync → #${wantedTmdbId} ${movie.title || movie.name}`)
            }
          } catch(e) {}
        }
        await selectHydracker(found)
      } else {
        hydrackerManualError = `Fiche #${id} introuvable sur Hydracker`
        addLog('HYD', `✗ ${hydrackerManualError}`)
      }
    } catch(e) {
      hydrackerManualError = String(e?.message || e || 'erreur inconnue')
      addLog('HYD', `✗ Erreur lookup ID ${id} : ${hydrackerManualError}`)
    }
    hydrackerManualLoading = false
  }

  function generateNFO() {
    if (!mediaInfo && !fileInfo && !selectedTMDB) return ''
    const W = 62
    const lines = []
    const pad = (k, v) => lines.push(`  ${k.padEnd(14)}: ${v}`)
    const section = (title) => {
      lines.push('')
      lines.push(`─── ${title} ${'─'.repeat(Math.max(0, W - 5 - title.length))}`)
    }
    const box = (text) => {
      const content = text.slice(0, W - 4)
      const inner = content.padEnd(W - 4)
      lines.push('╔' + '═'.repeat(W - 2) + '╗')
      lines.push(`║ ${inner} ║`)
      lines.push('╚' + '═'.repeat(W - 2) + '╝')
    }

    const title = selectedTMDB?.title || selectedTMDB?.name || selectedHydracker?.name || fileInfo?.title || ''
    const year = (selectedTMDB?.release_date || selectedTMDB?.first_air_date || '').slice(0, 4) || fileInfo?.year || ''
    box(year ? `${title} (${year})` : title || (file?.name || ''))

    section('GÉNÉRAL')
    if (file?.name)           pad('Fichier',    file.name)
    if (mediaInfo?.filesize)  pad('Taille',     mediaInfo.filesize)
    if (mediaInfo?.duration)  pad('Durée',      mediaInfo.duration)
    const qualName = qualityOptions.find(q => q.id === postQuality)?.name
    if (qualName)             pad('Qualité',    qualName)

    if (mediaInfo?.videoCodec || mediaInfo?.width) {
      section('VIDÉO')
      if (mediaInfo?.videoCodec) {
        const codec = mediaInfo.videoCodec + (mediaInfo.videoProfile ? ` (${mediaInfo.videoProfile})` : '')
        pad('Codec',      codec)
      }
      if (mediaInfo?.width && mediaInfo?.height) pad('Résolution', `${mediaInfo.width}×${mediaInfo.height}`)
      if (mediaInfo?.bitrate)   pad('Bitrate',    mediaInfo.bitrate)
    }

    if (mediaInfo?.audios?.length) {
      section('AUDIO')
      mediaInfo.audios.forEach((a, i) => {
        const val = [a.codec, a.channels, a.lang].filter(Boolean).join(' · ')
        pad(`Piste ${i + 1}`, val)
      })
      if (postLanguages.length) pad('Langues', postLanguages.map(l => l.name).join(', '))
    }

    if (mediaInfo?.subs?.length || postSubs.length) {
      section('SOUS-TITRES')
      if (postSubs.length) {
        postSubs.forEach(s => lines.push(`  • ${s.name}`))
      } else if (mediaInfo?.subs?.length) {
        mediaInfo.subs.forEach(s => lines.push(`  • ${s}`))
      }
    }

    if (selectedTMDB?.id) {
      section('RÉFÉRENCES')
      pad('TMDB', `#${selectedTMDB.id}`)
    }

    lines.push('')
    lines.push('─'.repeat(W))
    const sig = appVersion ? `GO POST TOOLS v${appVersion}` : 'GO POST TOOLS'
    lines.push(`  ${sig} · ${new Date().toLocaleDateString('fr-FR')}`)
    return lines.join('\n')
  }
  // Le NFO effectif : manuel si l'user en a saisi un, sinon généré auto.
  $: nfoPreview = postNfoManual.trim()
    ? postNfoManual
    : ((file && (mediaInfo || fileInfo)) ? generateNFO() : '')

  // --- Post ---
  async function lancerPost() {
    if (!selectedHydracker || !postQuality || !postLanguages.length) return
    if (!postUploadTypes.nzb && !postUploadTypes.ddl) {
      postResult = { ok: false, message: 'Choisis NZB ou DDL avant de lancer (aucun uploader coché)' }
      addLog('QUEUE', '✗ aucun uploader coché (NZB / DDL)')
      return
    }
    queueCancelled = false  // nouveau cycle de post — on lève le flag d'annulation
    postLoading = true
    postResult = null
    nzbStatus = ''; nzbParparPct = 0; nzbNyuuPct = 0; nzbNyuuSpeed = ''; nzbNyuuETA = ''; nzbNyuuArticles = ''
    ddlHosts = { '1Fichier': { active: false, filename: '', pct: 0, speed: '', done: false, posting: false, posted: false, hydrackerID: 0, error: '' } }

    // Pivot Elysium : les workflows Post{Nzb,DDL}Workflow prennent maintenant
    // (tmdbID, mediaType, quality, langues, subs, mkvPath, nfo, ...).
    const tmdbID    = selectedTMDB?.id || 0
    const mediaType = selectedTMDB?.media_type || (selectedHydracker?.type || 'movie')
    const qualityName = qualityOptions.find(q => q.id === postQuality || q.name === postQuality)?.name || ''
    // Elysium n'accepte QUE les noms de sa propre liste et renvoie sinon un
    // HTTP 422 « The selected language.0 is invalid », qui fait perdre le post
    // entier (constaté sur un VOSTFR, absent de sa liste). La pastille annonçait
    // déjà « ne sera pas envoyée » sans qu'aucun filtre n'existe : le voici.
    const nomsLangOk = new Set(langOptions.map(o => o.name))
    const nomsSubOk  = new Set(subOptions.map(o => o.name))
    const langNames  = postLanguages.map(l => l.name).filter(n => n && nomsLangOk.has(n))
    const subNames   = postSubs.map(s => s.name).filter(n => n && nomsSubOk.has(n))
    const langEcart  = postLanguages.map(l => l.name).filter(n => n && !nomsLangOk.has(n))
    const subEcart   = postSubs.map(s => s.name).filter(n => n && !nomsSubOk.has(n))
    if (langEcart.length) addLog('POST', `⚠ langue(s) écartée(s), hors liste Elysium : ${langEcart.join(', ')}`)
    if (subEcart.length)  addLog('POST', `⚠ sous-titre(s) écarté(s), hors liste Elysium : ${subEcart.join(', ')}`)
    // Le NFO reste wrappé <pre>+entités échappées : le backend le dé-wrappe
    // avant l'envoi à Elysium (nfoForElysium) — historique préservé.
    const nfoText = postNfoManual.trim() || generateNFO()
    const nfo     = nfoText ? `<pre>${nfoText.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')}</pre>` : ''
    const errors = []
    const successes = []

    // Scroll vers les barres de progression dès le lancement
    requestAnimationFrame(() => {
      document.getElementById('progress-anchor')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    })

    // Helper : relance une fois si l'appel échoue (sauf si l'utilisateur a cliqué Stop).
    // fn doit retourner une valeur truthy pour "OK" ; si throw OU falsy => retry.
    const withRetry = async (name, fn, successCheck) => {
      for (let attempt = 1; attempt <= 2; attempt++) {
        if (queueCancelled) throw new Error('annulé')
        try {
          const r = await fn()
          if (successCheck(r)) return r
          if (attempt === 2) throw new Error(`${name} : échec après retry`)
          addLog('QUEUE', `↻ Retry ${name} (tentative ${attempt + 1}/2)`)
        } catch(e) {
          if (queueCancelled) throw e
          if (attempt === 2) throw e
          addLog('QUEUE', `↻ Retry ${name} après erreur (${e}) — tentative ${attempt + 1}/2`)
        }
      }
    }

    const tasks = []
    if (postUploadTypes.nzb) {
      if (!mkvFilePath) errors.push('NZB : chemin du fichier introuvable — cliquez Parcourir')
      else if (!tmdbID) errors.push('NZB : aucune fiche TMDB sélectionnée')
      else tasks.push(
        withRetry(
          'NZB',
          () => PostNzbWorkflow(tmdbID, mediaType, qualityName, langNames, subNames, mkvFilePath, nfo, postSeason || 0, postEpisode || 0),
          r => !!r?.nzb_path,
        )
          .then(r => successes.push(`NZB Elysium #${r.elysium_id} ajouté`))
          .catch(e => errors.push(`NZB : ${e}`))
      )
    }
    if (postUploadTypes.ddl) {
      if (!mkvFilePath) errors.push('DDL : chemin MKV introuvable')
      else if (!tmdbID) errors.push('DDL : aucune fiche TMDB sélectionnée')
      else tasks.push(
        withRetry(
          'DDL',
          () => PostDDLWorkflow(tmdbID, mediaType, qualityName, langNames, subNames, mkvFilePath, nfo, postSeason || 0, postEpisode || 0),
          r => !!(r?.links?.length),
        )
          .then(r => {
            const fname = file?.name || ''
            const ep = (postSeason || postEpisode) ? ` S${String(postSeason).padStart(2,'0')}E${String(postEpisode).padStart(2,'0')}` : ''
            const linksStr = r.links.join(' · ')
            successes.push(`DDL Elysium #${r.elysium_id} ajouté (${r.links.length} lien${r.links.length > 1 ? 's' : ''}) — ${fname}${ep}\n${linksStr}`)
          })
          .catch(e => errors.push(`DDL : ${e}`))
      )
    }

    await Promise.all(tasks)

    if (successes.length && !errors.length) postResult = { ok: true, message: successes.join(' · ') }
    else if (successes.length) postResult = { ok: true, message: successes.join(' · '), details: errors.join(' | ') }
    else postResult = { ok: false, message: errors.join(' | ') }
    postLoading = false
    // Notif pour un post isolé (queue gère sa propre notif finale)
    if (!queueProcessing) {
      try { Notify(postResult.ok ? '✓ Post terminé' : '✗ Post échoué', file?.name || '') } catch(e) {}
    }
  }

  // Reset toutes les barres de progression (DDL / Torrent / NZB) à l'état initial
  function resetAllProgress() {
    nzbStatus = ''; nzbParparPct = 0; nzbNyuuPct = 0; nzbNyuuSpeed = ''; nzbNyuuETA = ''; nzbNyuuArticles = ''
    ddlHosts = {
      '1Fichier': { active: false, filename: '', pct: 0, speed: '', done: false, posting: false, posted: false, hydrackerID: 0, error: '' },
    }
  }

  async function stopPost() {
    // Flag d'annulation maintenu jusqu'au prochain lancerPost/processQueue
    // (ne PAS reset via setTimeout — sinon un retry en cours voit le flag
    // effacé et relance un workflow que l'utilisateur vient d'annuler).
    queueCancelled = true
    queue = []
    queueTMDBHint = 0
    queueTMDBHintType = ''
    queueQualityHint = ''
    queueLangHint = null
    queueSubHint = null
    try { await CancelAllWorkflows() } catch(e) {}
    postLoading = false
    queueProcessing = false
    queueCurrent = ''
    resetAllProgress()
    postResult = { ok: false, message: 'Arrêté par l\'utilisateur' }
    addLog('QUEUE', '■ Stop — tout arrêté, queue vidée')
  }

  // --- Recherche fiche UNIFIÉE (Elysium + TMDB) — un seul champ, un seul
  // bouton, résultats mergés avec un badge « déjà sur Elysium » quand la
  // fiche est déjà présente sur le site. Remplace les 2 blocs séparés
  // depuis 2026-09-14.
  let unifiedResults = []      // [{source, tmdb_id, media_type, title, year, poster_url, badge}]
  let unifiedLoading = false

  async function searchUnified() {
    const q = tmdbSearchQuery.trim()
    if (!q) return
    unifiedLoading = true
    unifiedResults = []
    hydrackerResults = []
    tmdbResults = []
    igdbResults = []
    try {
      // Mode Jeu = IGDB seul (Elysium n'a que des fiches IGDB internes déjà couvertes par sélection jeu ailleurs).
      if (tmdbSearchType === 'game') {
        igdbResults = await IgdbSearch(q) || []
        addLog('IGDB', `Recherche "${q}" : ${igdbResults.length} jeu(x)`)
        if (igdbResults.length === 1) await selectGame(igdbResults[0])
        unifiedLoading = false
        return
      }
      const mt = tmdbSearchType  // 'movie' | 'tv'
      // Parallèle : Elysium (déjà présents) + TMDB (candidats à importer).
      const [elyRaw, tmdbRaw] = await Promise.all([
        ElysiumSearchTitles(q, mt).catch(() => []),
        TMDBSearch(q).catch(() => []),
      ])
      const elyKey = new Set()
      const merged = []
      // 1. Elysium en premier (badge « déjà présente »)
      for (const t of (elyRaw || [])) {
        if (!t.tmdb_id) continue
        const key = `${t.type || 'movie'}:${t.tmdb_id}`
        elyKey.add(key)
        merged.push({
          source: 'elysium',
          tmdb_id: t.tmdb_id,
          media_type: t.type || 'movie',
          title: t.title,
          year: t.year || 0,
          poster_url: t.poster_url || '',
          badge: '✓ Elysium',
        })
      }
      // 2. TMDB filtré par type, dédupliqué contre Elysium
      for (const m of (tmdbRaw || [])) {
        const kind = m.media_type || mt
        if (kind !== mt) continue
        const key = `${kind}:${m.id}`
        if (elyKey.has(key)) continue
        merged.push({
          source: 'tmdb',
          tmdb_id: m.id,
          media_type: kind,
          title: m.title || m.name || '',
          year: parseInt((m.release_date || m.first_air_date || '').slice(0, 4)) || 0,
          poster_url: m.poster_path ? `https://image.tmdb.org/t/p/w185${m.poster_path}` : '',
          badge: 'TMDB',
        })
      }
      unifiedResults = merged
      addLog('SEARCH', `"${q}" (${mt}) : ${elyRaw?.length || 0} Elysium + ${tmdbRaw?.length || 0} TMDB → ${merged.length} résultats`)
      // 1 seul résultat → sélection auto
      if (merged.length === 1) await pickUnified(merged[0])
    } catch (e) {
      console.error(e)
      addLog('SEARCH', `✗ ${e?.message || e}`)
    }
    unifiedLoading = false
  }

  // pickUnified : sélectionne un résultat unifié — construit un pseudo-movie
  // TMDB et laisse selectTMDB faire la chaîne (poster + HydrackerGetByTmdbID
  // qui, via l'adapter, tente Elysium puis import auto si besoin).
  async function pickUnified(r) {
    unifiedResults = []
    const pseudoMovie = {
      id: r.tmdb_id,
      media_type: r.media_type,
      title: r.title,
      name: r.title,
      release_date: r.year ? `${r.year}-01-01` : '',
      first_air_date: r.year ? `${r.year}-01-01` : '',
      poster_path: '',
      _poster_full: r.poster_url,
      _from_mediasearch: true,
    }
    await selectTMDB(pseudoMovie)
  }

  // Alias legacy pour compat (Cmd+K et autres callsites) — redirige vers unifié.
  async function searchHydracker() { return searchUnified() }

  async function selectHydracker(title) {
    selectedHydracker = title
    hydrackerResults = []
    hydrackerPosterUrl = ''
    if (title.poster) {
      try { hydrackerPosterUrl = await FetchImageBase64(title.poster) } catch(e) {}
    }
  }

  async function deselectHydracker() {
    selectedHydracker = null
    hydrackerPosterUrl = ''
    hydrackerResults = []
    // Désélection manuelle = l'utilisateur veut créer/rechercher une autre fiche.
    // On affiche direct le popup de création (avec ID TMDB + "Ouvrir Admin" + input
    // pour coller l'ID Hydracker). Le champ "Rechercher sur Hydracker" reste
    // dispo en dessous si besoin.
    hydrackerNotFound = Boolean(selectedTMDB)
    hydrackerManualId = ''
    hydrackerManualError = ''
  }

  // Mémorise les pistes choisies comme référence pour la suite de la
  // queue. Appelée APRÈS chaque modification manuelle (l'affectation
  // Svelte est synchrone, les tableaux sont donc déjà à jour).
  // Ne fait rien hors queue : un post isolé n'a rien à propager.
  function rememberQueueTracks() {
    if (queue.length > 0 || queueProcessing) {
      queueLangHint = [...postLanguages]
      queueSubHint = [...postSubs]
      addLog('QUEUE', `↺ pistes mémorisées pour la suite : `
        + `${postLanguages.map(l => l.name).join(', ') || '—'}`
        + ` / ${postSubs.map(s => s.name).join(', ') || '—'}`)
    }
  }

  function addLang(opt) {
    if (!postLanguages.find(l => l.id === opt.id)) postLanguages = [...postLanguages, opt]
    rememberQueueTracks()
  }

  function addSub(opt) {
    if (!postSubs.find(s => s.id === opt.id)) postSubs = [...postSubs, opt]
    rememberQueueTracks()
  }
  function removeSubAt(idx) { postSubs = postSubs.filter((_, i) => i !== idx); rememberQueueTracks() }
  function removeLangAt(idx) { postLanguages = postLanguages.filter((_, i) => i !== idx); rememberQueueTracks() }
</script>

<div class="hydracker-tab">

  <!-- Queue batch -->
  {#if queueProcessing || queue.length > 0}
    <div class="queue-bar">
      <div class="queue-head">
        <span class="queue-title">⚡ Queue batch</span>
        <span class="queue-counter">{queueDone} fait{queueDone > 1 ? 's' : ''} · {queue.length + (queueProcessing ? 1 : 0)} en attente</span>
        <button class="btn-test" on:click={clearQueue} disabled={!queue.length}>Vider</button>
      </div>
      {#if queue.length > 0}
        <div class="queue-list">
          {#each queue as path, i}
            <div class="queue-item">
              <span class="queue-idx">#{i + 1}</span>
              <code class="queue-name">{path.split('/').pop()}</code>
              <button class="btn-x" on:click={() => dequeueAt(i)}>✕</button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Modal multi-MKV : choisir queue ou saison complète -->
  {#if pendingMultiPaths}
    <div class="multi-modal-bg" on:click|self={() => pendingMultiPaths = null}>
      <div class="multi-modal">
        <div class="multi-modal-title">📚 {pendingMultiPaths.length} fichiers sélectionnés</div>
        <div class="multi-modal-msg">Comment veux-tu les traiter ?</div>
        <div class="multi-modal-actions">
          <button class="multi-btn multi-btn-saison" on:click={async () => {
            const paths = pendingMultiPaths
            pendingMultiPaths = null
            try {
              const tempFolder = await PrepareSeasonFolder(paths)
              loadFileFromPath(tempFolder, null, { isFolder: true })
              addLog('QUEUE', `📚 ${paths.length} MKV regroupés en saison → ${tempFolder.split('/').pop()}`)
            } catch(e) { addLog('QUEUE', `✗ PrepareSeasonFolder : ${e}`) }
          }}>
            <div class="multi-btn-emoji">📦</div>
            <div class="multi-btn-label">Saison complète</div>
            <div class="multi-btn-hint">1 post combiné, full_saison=1</div>
          </button>
          <button class="multi-btn multi-btn-queue" on:click={() => {
            const paths = pendingMultiPaths
            pendingMultiPaths = null
            paths.forEach(p => enqueue(p))
          }}>
            <div class="multi-btn-emoji">📋</div>
            <div class="multi-btn-label">Queue</div>
            <div class="multi-btn-hint">1 post par épisode</div>
          </button>
        </div>
        <button class="multi-cancel" on:click={() => pendingMultiPaths = null}>Annuler</button>
      </div>
    </div>
  {/if}

  <!-- Zone de drop -->
  {#if !file}
  <div class="dropzone" class:drag={dragOver}
    on:dragover={onDragOver} on:dragleave={onDragLeave}
    role="region" aria-label="Zone de dépôt">
    <div class="drop-icon"><svg class="drop-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 5.25h16.5v13.5H3.75zM3.75 9h16.5M3.75 15h16.5M8.25 5.25v13.5M15.75 5.25v13.5"/></svg></div>
    <p>Glissez un fichier <strong>.mkv</strong> ici</p>
    <p class="drop-sub">ou pour une saison complète, glissez un <strong>dossier</strong> / archive</p>
    <div class="drop-buttons">
      <button class="btn-browse" on:click={async () => {
        const paths = await SelectMkvFiles()
        if (!paths?.length) return
        if (paths.length === 1) {
          loadFileFromPath(paths[0], null)
          return
        }
        // Multi-fichiers : ouvre le modal pour choisir queue ou saison
        pendingMultiPaths = paths
      }}><svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14.25 3.75H6.75A1.5 1.5 0 0 0 5.25 5.25v13.5a1.5 1.5 0 0 0 1.5 1.5h10.5a1.5 1.5 0 0 0 1.5-1.5V8.25l-4.5-4.5ZM14.25 3.75V8.25h4.5"/></svg>Parcourir MKV</button>
      <button class="btn-browse" on:click={async () => {
        const path = await SelectFolder()
        if (path) loadFileFromPath(path, null, { isFolder: true })
      }}>📁 Dossier saison</button>
      <button class="btn-browse" on:click={async () => {
        const path = await SelectArchiveFile()
        if (path) loadFileFromPath(path, null, { isArchive: true })
      }}>📦 Archive (DDL)</button>
    </div>
  </div>

  {:else}
  <!-- Fond d'ambiance : l'affiche de la release, très floutée et assombrie.
       Chaque titre donne ainsi sa couleur à l'écran. Rendu STATIQUE (aucune
       animation dessous), et le flou est figé sur une image immobile : pas de
       recalcul par image, contrairement au backdrop-filter sur fond animé qui
       avait mis le cockpit d'encodage à 50 % de CPU. -->
  {#if posterDataUrl}
    <div class="ambiance" style="background-image:url({posterDataUrl})" aria-hidden="true"></div>
  {/if}
  <div class="main-grid" class:reveal={!!file}>

    <!-- Colonne gauche : poster + infos fiche -->
    <div class="col-left">
      <!-- Poster TMDB -->
      <div class="poster-wrap">
        {#if posterDataUrl}
          <img src={posterDataUrl} alt={selectedTMDB?.title || selectedTMDB?.name} />
        {:else}
          <div class="poster-placeholder"><svg class="ph-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 5.25h16.5v13.5H3.75zM3.75 9h16.5M3.75 15h16.5M8.25 5.25v13.5M15.75 5.25v13.5"/></svg></div>
        {/if}
      </div>
      {#if selectedTMDB}
        <div class="fiche-title">{selectedTMDB.title || selectedTMDB.name}</div>
        <div class="fiche-year">{(selectedTMDB.release_date || selectedTMDB.first_air_date || '').slice(0,4)}</div>
        <button class="btn-ghost-sm" on:click={() => { selectedTMDB = null; tmdbResults = [] }}>
          Changer de fiche
        </button>
      {/if}

      <!-- Confirmation fiche Hydracker -->
      {#if selectedHydracker}
        <div class="selected-hyd">
          {#if hydrackerPosterUrl}
            <img class="hyd-poster" src={hydrackerPosterUrl} alt={selectedHydracker.name} />
          {:else}
            <div class="hyd-poster hyd-poster-placeholder"><svg class="ph-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 5.25h16.5v13.5H3.75zM3.75 9h16.5M3.75 15h16.5M8.25 5.25v13.5M15.75 5.25v13.5"/></svg></div>
          {/if}
          <div class="hyd-info">
            <div class="hyd-info-name">✓ {selectedHydracker.name}</div>
            <div class="hyd-info-meta">
              {#if selectedHydracker.release_date}{selectedHydracker.release_date.slice(0,4)} · {/if}
              <span class="hyd-type badge-{selectedHydracker.type}">{selectedHydracker.type}</span>
            </div>
          </div>
          <button class="btn-x" style="margin-left:auto" on:click={deselectHydracker}>✕</button>
        </div>
      {:else if hydrackerNotFound && selectedTMDB}
        <div class="hyd-create-box">
          <div class="hyd-create-title">⚠ Fiche Hydracker introuvable</div>
          <div class="hyd-create-hint">ID TMDB (modifiable si auto-detect erroné) :</div>
          <div class="hyd-tmdb-url-row">
            <input type="number" class="hyd-tmdb-input" bind:value={manualTmdbIdEdit}
              on:keydown={e => e.key === 'Enter' && refreshTmdbFromManual()} />
            <button class="btn-copy" on:click={() => navigator.clipboard.writeText(String(manualTmdbIdEdit || selectedTMDB.id))}>{@html COPY_SVG}</button>
            <button class="btn-copy" title="Mettre à jour la fiche TMDB depuis cet ID" on:click={refreshTmdbFromManual} disabled={tmdbReloadLoading}>
              {tmdbReloadLoading ? '…' : '🔄'}
            </button>
          </div>
          <div class="hyd-create-hint" style="margin-top:10px">Elysium créera la fiche automatiquement au moment du post — ou entrez un ID TMDB pour la charger tout de suite :</div>
          <div class="hyd-type-toggle">
            <label class:active={hydrackerManualType === 'movie'}>
              <input type="radio" bind:group={hydrackerManualType} value="movie" /> 🎬 Film
            </label>
            <label class:active={hydrackerManualType === 'tv'}>
              <input type="radio" bind:group={hydrackerManualType} value="tv" /> 📺 Série
            </label>
          </div>
          <div class="hyd-id-row">
            <input type="number" bind:value={hydrackerManualId} placeholder="ID fiche Hydracker"
              on:keydown={e => e.key === 'Enter' && confirmHydrackerID()} />
            <button class="btn-search" on:click={confirmHydrackerID} disabled={hydrackerManualLoading}>
              {hydrackerManualLoading ? '…' : 'Valider'}
            </button>
          </div>
          {#if hydrackerManualError}
            <div class="hyd-manual-error">✗ {hydrackerManualError}</div>
          {/if}
        </div>
      {/if}

      <!-- Infos MediaInfo (repliable pour gagner de la place) -->
      {#if mediaInfoLoading}
        <div class="mi-loading">Analyse MediaInfo…</div>
      {:else if mediaInfoError}
        <div class="mi-error">
          <strong>ℹ MediaInfo indisponible</strong> — les langues et sous-titres sont à saisir à la main.
          Le post peut continuer normalement.
          <div class="mi-error-detail">Détail : {mediaInfoError}</div>
        </div>
      {:else if mediaInfo}
        <div class="mi-details" class:open={mediaInfoOpen}>
          <button type="button" class="mi-summary" on:click={() => mediaInfoOpen = !mediaInfoOpen}>
            <span class="mi-chevron">{mediaInfoOpen ? '▾' : '▸'}</span>
            🎞️ {mediaInfo.videoCodec || ''}{mediaInfo.width ? ` · ${mediaInfo.width}p` : ''}{mediaInfo.filesize ? ` · ${mediaInfo.filesize}` : ''}{mediaInfo.duration ? ` · ${mediaInfo.duration}` : ''}
          </button>
          {#if mediaInfoOpen}
            <div class="mi-block">
              {#if mediaInfo.filesize}<div class="mi-row"><span>Taille</span><span>{mediaInfo.filesize}</span></div>{/if}
              {#if mediaInfo.duration}<div class="mi-row"><span>Durée</span><span>{mediaInfo.duration}</span></div>{/if}
              {#if mediaInfo.videoCodec}<div class="mi-row"><span>Vidéo</span><span>{mediaInfo.videoCodec}{mediaInfo.videoProfile ? ' ' + mediaInfo.videoProfile : ''}</span></div>{/if}
              {#if mediaInfo.width && mediaInfo.height}<div class="mi-row"><span>Résolution</span><span>{mediaInfo.width}×{mediaInfo.height}</span></div>{/if}
              {#if mediaInfo.bitrate}<div class="mi-row"><span>Bitrate</span><span>{mediaInfo.bitrate}</span></div>{/if}
              {#each mediaInfo.audios as a}
                <div class="mi-row"><span>Audio{a.lang ? ' ('+a.lang+')' : ''}</span><span>{a.codec}{a.channels ? ' '+a.channels : ''}</span></div>
              {/each}
              {#if mediaInfo.subs.length}
                <div class="mi-row"><span>Sous-titres</span><span>{mediaInfo.subs.join(', ')}</span></div>
              {/if}
            </div>

            <!-- Pistes détectées : confirmation visuelle Title → mapping Hydracker -->
            {#if (mediaInfo.audios?.length || mediaInfo.subTracks?.length)}
              <div class="mi-block tracks-block">
                <div class="tracks-header"><svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Zm0-4.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9Zm0-3a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z"/></svg>Pistes détectées (mapping auto Elysium)</div>
                {#each (mediaInfo.audios || []) as a, i}
                  {@const m = mapAudioTrackByTitle(a)}
                  <div class="track-row">
                    <span class="track-badge track-badge-audio">AUDIO #{i+1}</span>
                    <span class="track-title">{a.title || a.language || '(sans titre)'}</span>
                    <span class="track-arrow">→</span>
                    <!-- Post-pivot Elysium : les IDs numériques ne sont plus utilisés
                         (l'API accepte les noms). On n'affiche le ⚠ que si le mapping
                         a échoué (name manquant ou "?"). -->
                    <span class="track-mapped" class:track-mapped-warn={!m?.name || m.name === '?'}>{m?.name || '?'}{(!m?.name || m.name === '?') ? ' ⚠' : ''}</span>
                  </div>
                {/each}
                {#each (mediaInfo.subTracks || []) as t, i}
                  {@const m = t.forced ? null : mapSubTrackByTitle(t)}
                  <div class="track-row" class:track-row-muted={t.forced}>
                    <span class="track-badge track-badge-sub">SUB #{i+1}</span>
                    <span class="track-title">{t.title || t.language || '(sans titre)'}{t.forced ? ' (forced)' : ''}</span>
                    <span class="track-arrow">→</span>
                    <span class="track-mapped" class:track-mapped-warn={!t.forced && (!m?.name || m.name === '?')}>{t.forced ? '— (filtré)' : (m?.name || '?')}{!t.forced && (!m?.name || m.name === '?') ? ' ⚠' : ''}</span>
                  </div>
                {/each}
              </div>
            {/if}
          {/if}
        </div>
      {/if}
      {#if selectedTMDB}
        <!-- Bilan de session : ce qui est réellement arrivé sur Elysium -->
        {#if sessionChecking}
          <div class="bilan-box"><div class="bilan-head"><span>Vérification sur Elysium…</span></div></div>
        {:else if sessionReport}
          <div class="bilan-box" class:bilan-ko={sessionReport.some(x => x.voulu.nzb && !x.nzb)}>
            <div class="bilan-head">
              <span>Bilan de session</span>
              <span class="bilan-count">
                {sessionReport.filter(x => (!x.voulu.nzb || x.nzb) && (!x.voulu.ddl || x.ddl)).length}/{sessionReport.length} complets
              </span>
              <button class="bilan-x" title="Fermer" on:click={() => sessionReport = null}>×</button>
            </div>
            <div class="bilan-list">
              {#each sessionReport as r}
                <div class="bilan-row">
                  <span class="bilan-name">{r.filename}</span>
                  {#if r.voulu.nzb}<span class="bilan-tag" class:bilan-on={r.nzb} class:bilan-off={!r.nzb}>NZB</span>{/if}
                  {#if r.voulu.ddl}<span class="bilan-tag" class:bilan-on={r.ddl} class:bilan-off={!r.ddl}>DDL</span>{/if}
                </div>
              {/each}
            </div>
            {#if sessionReport.filter(x => x.voulu.nzb && !x.nzb && x.path).length > 0}
              <button class="bilan-fix" on:click={reposterNzbManquants}>
                ↻ Reposter les {sessionReport.filter(x => x.voulu.nzb && !x.nzb && x.path).length} NZB manquants — DDL désactivé
              </button>
            {/if}
          </div>
        {/if}

        <!-- Récapitulatif du post (repliable) -->
        <div class="recap-box" class:open={recapOpen}>
          <button type="button" class="recap-title" on:click={() => recapOpen = !recapOpen}>
            <span class="mi-chevron">{recapOpen ? '▾' : '▸'}</span>
            <svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M8.25 6.75h10.5M8.25 12h10.5M8.25 17.25h10.5M4.5 6.75h.008M4.5 12h.008M4.5 17.25h.008"/></svg>Récapitulatif du post Elysium
          </button>
          {#if recapOpen}
            <div class="recap-body">
              <div class="recap-row">
                <span class="recap-key">Titre</span>
                <span class="recap-val">{selectedTMDB.title || selectedTMDB.name}
                  {#if (selectedTMDB.release_date || selectedTMDB.first_air_date)}
                    ({(selectedTMDB.release_date || selectedTMDB.first_air_date).slice(0,4)})
                  {/if}
                </span>
              </div>
              <div class="recap-row">
                <span class="recap-key">TMDB ID</span>
                <span class="recap-val recap-id">{selectedTMDB.id} <span class="recap-type">{selectedTMDB.media_type || 'movie'}</span></span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Fiche Elysium</span>
                <span class="recap-val">{selectedHydracker ? selectedHydracker.name + ' (#' + (selectedHydracker.id || '?') + ')' : '— non sélectionnée'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Qualité</span>
                <span class="recap-val">{qualityOptions.find(q => q.id === postQuality)?.name || '—'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Codec vidéo</span>
                <span class="recap-val">{mediaInfo?.videoCodec || fileInfo?.video_codec || '—'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Codec audio</span>
                <span class="recap-val">{mediaInfo?.audios?.[0]?.codec || fileInfo?.audio_codec || '—'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Langues</span>
                <span class="recap-val">{postLanguages.length ? postLanguages.map(l => l.name).join(', ') : '—'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Sous-titres</span>
                <span class="recap-val">{postSubs.length ? postSubs.map(s => s.name).join(', ') : '—'}</span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Upload via</span>
                <span class="recap-val">
                  {(() => {
                    const parts = []
                    if (postUploadTypes.nzb) parts.push('NZB')
                    if (postUploadTypes.ddl) parts.push('DDL (1Fichier)')
                    return parts.join(' + ') || '—'
                  })()}
                </span>
              </div>
              <div class="recap-row">
                <span class="recap-key">Fichier</span>
                <span class="recap-val recap-file">{file?.name}</span>
              </div>
            </div>
          {/if}
        </div>
      {/if}

    </div>

    <!-- Colonne droite : recherches + post options -->
    <div class="col-right">
      {#if queueCurrent || postLoading || queueProcessing}
        <div class="release-current" class:has-spine={postUploadTypes.nzb}>
          {#if postUploadTypes.nzb}
            <!-- Épine : les 4 phases du post NZB, pilotées par nzb:status.
                 Affichée seulement si le NZB fait partie du post — sur un
                 DDL seul, ces phases n'existent pas et afficheraient un
                 état figé trompeur. -->
            <div class="spine" title={POST_PHASES[postPhase] || 'Terminé'}>
              {#each POST_PHASES as ph, i}
                <div class="ph" class:done={i < postPhase} class:now={i === postPhase}>
                  <span class="ph-dot"></span>
                  {#if i < POST_PHASES.length - 1}<span class="ph-stem"></span>{/if}
                </div>
              {/each}
            </div>
          {/if}
          <div class="release-body">
            <div class="release-line">
              <span class="release-current-label">▶ {queueProcessing ? 'Release en cours' : 'Post en cours'}</span>
              <code class="release-current-name">{(queueCurrent || file?.name || '').split('/').pop()}</code>
              {#if queueProcessing && queue.length > 0}
                <button class="btn-skip-ep" on:click={async () => { try { await SkipCurrentEpisode() } catch(e) {} }} title="Abandonner cet épisode et passer au suivant dans la queue">
                  ⏭ Skip épisode
                </button>
              {/if}
              <button class="btn-stop-inline" on:click={stopPost} title="Tout arrêter">■ Stop</button>
            </div>
            {#if postUploadTypes.nzb}
              <div class="release-phases">
                {#each POST_PHASES as ph, i}
                  <span class="phl" class:done={i < postPhase} class:now={i === postPhase}
                    >{ph}{i < postPhase ? ' ✓' : i === postPhase ? ' ▶' : ''}</span
                  >{#if i < POST_PHASES.length - 1}<span class="phsep">·</span>{/if}
                {/each}
              </div>
            {/if}
          </div>
        </div>
      {/if}

      <!-- Fichier détecté -->
      <div class="file-badge">
        <svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14.25 3.75H6.75A1.5 1.5 0 0 0 5.25 5.25v13.5a1.5 1.5 0 0 0 1.5 1.5h10.5a1.5 1.5 0 0 0 1.5-1.5V8.25l-4.5-4.5ZM14.25 3.75V8.25h4.5"/></svg>{file.name}
        <button class="btn-x" on:click={() => { file = null; fileInfo = null; selectedTMDB = null }}>✕</button>
      </div>

      <!-- Actions principales (Lancer / Stop / Réinitialiser) juste sous le fichier -->
      <div class="post-actions">
        <button class="btn-start" title={!postUploadTypes.nzb && !postUploadTypes.ddl ? 'Choisis NZB ou DDL avant de lancer' : '⌘↵'}
          disabled={postLoading || queueProcessing || (!postUploadTypes.nzb && !postUploadTypes.ddl) || (queue.length === 0 && (!postQuality || !postLanguages.length || !selectedHydracker))}
          on:click={() => queue.length > 0 ? processQueue() : lancerPost()}>
          {postLoading || queueProcessing ? '…' : (queue.length > 0 ? `▶ Lancer la queue (${queue.length})` : '▶ Lancer')}
        </button>
        {#if postLoading || queueProcessing}
          <button class="btn-cancel" on:click={stopPost}>■ Stop</button>
        {/if}
        <button class="btn-reset" on:click={async () => {
          await stopPost()
          await new Promise(r => setTimeout(r, 300))
          queueResults = []; queueDone = 0; queueTotal = 0
          file = null; fileInfo = null; selectedTMDB = null;
          selectedHydracker = null; mediaInfo = null; posterDataUrl = ''; hydrackerPosterUrl = '';
          postQuality = ''; postLanguages = []; postSubs = [];
          postSeason = 0; postEpisode = 0; postFullSaison = false;
          mkvFilePath = ''; postResult = null;
        }}>↺ Réinitialiser</button>
      </div>

      <!-- Résumé du parser -->
      {#if fileInfo}
        <div class="parser-row">
          {#if fileInfo.title}<span class="tag tag-title">{fileInfo.title}</span>{/if}
          {#if fileInfo.year}<span class="tag">{fileInfo.year}</span>{/if}
          {#if fileInfo.quality}<span class="tag tag-qual">{fileInfo.quality}</span>{/if}
          {#if fileInfo.source}<span class="tag">{fileInfo.source}</span>{/if}
          {#if fileInfo.video_codec}<span class="tag tag-codec">{fileInfo.video_codec}</span>{/if}
          {#if fileInfo.audio_codec}<span class="tag tag-codec">{fileInfo.audio_codec}</span>{/if}
          {#each (fileInfo.languages || []) as l}<span class="tag tag-lang">{l}</span>{/each}
        </div>
      {/if}

      <!-- Ambiguïté TMDB -->
      {#if tmdbAmbiguous && tmdbResults.length > 0}
        <div class="ambig-box">
          <div class="ambig-title">
            <svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m0 3.5h.008M10.34 3.94 2.7 16.12A1.5 1.5 0 0 0 4 18.4h16a1.5 1.5 0 0 0 1.3-2.28L13.66 3.94a1.5 1.5 0 0 0-2.62 0Z"/></svg>
            {tmdbResults.length > 1 ? 'Plusieurs résultats — choisis la bonne fiche :' : 'Vérifie que c’est bien la bonne fiche :'}
          </div>
          <div class="ambig-list">
            {#each tmdbResults.slice(0,8) as m}
              <button class="ambig-item" on:click={() => selectTMDB(m)}>
                {#if m.poster_path}
                  <img src="https://image.tmdb.org/t/p/w92{m.poster_path}" alt="" />
                {:else if m._poster_full}
                  <img src={m._poster_full} alt="" />
                {:else}
                  <div class="ambig-no-poster"><svg class="ph-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 5.25h16.5v13.5H3.75zM3.75 9h16.5M3.75 15h16.5M8.25 5.25v13.5M15.75 5.25v13.5"/></svg></div>
                {/if}
                <div>
                  <div class="ambig-name">{m.title || m.name}</div>
                  <div class="ambig-year">{(m.release_date || m.first_air_date || '').slice(0,4)} · {m.media_type}</div>
                </div>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Recherche fiche UNIFIÉE : Elysium (déjà présentes) + TMDB (à importer)
           dans un seul champ. Remplace les 2 blocs séparés depuis 2026-09-14. -->
      <div class="searches-row" style="grid-template-columns: 1fr">
        <div class="search-section">
          <div class="search-label"><svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 1 1-13 0 6.5 6.5 0 0 1 13 0Z"/></svg>Recherche fiche</div>
          <div class="search-row">
            <div class="tmdb-type-toggle">
              <button type="button" class:active={tmdbSearchType === 'movie'} on:click={() => { tmdbSearchType = 'movie'; if (tmdbSearchQuery.trim()) searchUnified() }}><svg class="tt-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 5.25h16.5v13.5H3.75zM3.75 9h16.5M3.75 15h16.5M8.25 5.25v13.5M15.75 5.25v13.5"/></svg><span>Film</span></button>
              <button type="button" class:active={tmdbSearchType === 'tv'} on:click={() => { tmdbSearchType = 'tv'; if (tmdbSearchQuery.trim()) searchUnified() }}><svg class="tt-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 6.75h16.5v10.5H3.75zM8.25 20.25h7.5M12 17.25v3"/></svg><span>Série</span></button>
              <button type="button" class:active={tmdbSearchType === 'game'} on:click={() => { tmdbSearchType = 'game'; if (tmdbSearchQuery.trim()) searchUnified() }}><svg class="tt-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M7.5 10.5h3M9 9v3M15 10.5h.008M17.25 12.75h.008M6.75 16.5h10.5a3.75 3.75 0 0 0 0-7.5H6.75a3.75 3.75 0 0 0 0 7.5Z"/></svg><span>Jeu</span></button>
            </div>
            <input type="text" class="hyd-search-input" bind:value={tmdbSearchQuery} placeholder={gameMode ? 'Nom du jeu (⌘K)' : 'Nom du film / série (⌘K)'}
              on:keydown={e => e.key === 'Enter' && searchUnified()} />
            <input type="text" bind:value={tmdbSearchId} placeholder={gameMode ? 'ID IGDB' : 'ID TMDB'} style="width:90px;flex:none"
              on:keydown={e => e.key === 'Enter' && manualTMDBSearch()} />
            <button class="btn-search" on:click={searchUnified} disabled={unifiedLoading}>
              {unifiedLoading ? '…' : 'Chercher'}
            </button>
          </div>
          {#if rechercheMessage}
            <div class="search-msg">
              <svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 9v3.75m0 3.5h.008M10.34 3.94 2.7 16.12A1.5 1.5 0 0 0 4 18.4h16a1.5 1.5 0 0 0 1.3-2.28L13.66 3.94a1.5 1.5 0 0 0-2.62 0Z"/></svg>
              <span>{rechercheMessage}</span>
              <button type="button" class="search-msg-x" on:click={() => rechercheMessage = ''}>✕</button>
            </div>
          {/if}
          {#if unifiedResults.length > 0}
            <div class="hydracker-results">
              {#each unifiedResults as r}
                <button class="hydracker-item" on:click={() => pickUnified(r)}>
                  <span class="hyd-name">{r.title}</span>
                  <span class="hyd-year">{r.year || ''}</span>
                  <span class="hyd-type badge-{r.source === 'elysium' ? 'movie' : 'tv'}" style="background:{r.source === 'elysium' ? 'rgba(126,240,192,0.15)' : 'rgba(255,255,255,0.06)'};color:{r.source === 'elysium' ? '#7ef0c0' : 'var(--text2)'}">{r.badge}</span>
                </button>
              {/each}
            </div>
          {/if}
          {#if igdbResults.length > 1}
            <div class="hydracker-results">
              {#each igdbResults as g}
                <button class="hydracker-item" on:click={() => selectGame(g)}>
                  <span class="hyd-name">{g.name}</span>
                  <span class="hyd-year">{g.Year || ''}</span>
                  <span class="hyd-type badge-game">🎮 IGDB #{g.id}</span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <!-- Options de post -->
      {#if selectedTMDB}
        <div class="post-options">
          <div class="post-label">Options de post</div>

          <div class="post-grid">
            <!-- Colonne gauche -->
            <div class="post-field">
              <label for="post-quality">Qualité</label>
              <select id="post-quality" bind:value={postQuality} on:change={() => {
                qualityAutoFilled = false
                if (queue.length > 0 || queueProcessing) queueQualityHint = postQuality
              }}>
                <option value="">-- Choisir --</option>
                {#each qualityOptions as q}
                  <option value={q.id}>{q.name}</option>
                {/each}
              </select>
            </div>

            <!-- Colonne droite -->
            {#if selectedHydracker?.type === 'tv' || postSeason > 0 || postEpisode > 0}
              <div class="post-field">
                <div class="post-field-label">Saison / Épisode</div>
                <div class="se-row">
                  <label class="se-label">Saison <input type="number" min="0" class="se-input" bind:value={postSeason}
                    on:change={() => {
                      // Ancre queue : mémorise le mapping raw→(S,E) tant qu'on
                      // est en batch, pour propager l'offset aux fichiers suivants.
                      if ((queue.length > 0 || queueProcessing) && fileInfo?.episode) {
                        queueEpisodeAnchor = { rawEp: fileInfo.episode, saison: postSeason, episode: postEpisode }
                      }
                    }} /></label>
                  <label class="se-label">Épisode <input type="number" min="0" class="se-input" bind:value={postEpisode} disabled={postFullSaison} title={postFullSaison ? 'Désactivé : saison complète' : ''}
                    on:change={() => {
                      if ((queue.length > 0 || queueProcessing) && fileInfo?.episode) {
                        queueEpisodeAnchor = { rawEp: fileInfo.episode, saison: postSeason, episode: postEpisode }
                      }
                    }} /></label>
                </div>
                <label class="full-saison-toggle">
                  <input type="checkbox" bind:checked={postFullSaison} />
                  <span>📦 Saison complète</span>
                  <span class="full-saison-hint">{postFullSaison ? '(le post couvre toute la saison)' : ''}</span>
                </label>
              </div>
            {:else}<div></div>{/if}

            <div class="post-field">
              <div class="post-field-label">Langues</div>
              <div class="chips-row">
                {#each postLanguages as l, i}
                  <span class="chip" class:chip-unknown={l.id === 0} title={l.id === 0 ? 'Langue non reconnue — ne sera pas envoyée' : ''}>{l.name}{#if l.id === 0} ⚠{/if}<button class="chip-x" on:click={() => removeLangAt(i)}>✕</button></span>
                {/each}
                {#if !postLanguages.length}<span class="chips-empty">Aucune détectée</span>{/if}
              </div>
              <div class="add-row">
                <select bind:value={langSelectValue}>
                  <option value={null}>— Ajouter une langue —</option>
                  {#each langOptions.filter(o => !postLanguages.find(l => l.id === o.id)) as o}
                    <option value={o}>{o.name}</option>
                  {/each}
                </select>
                <button class="btn-add" on:click={() => { if (langSelectValue) { addLang(langSelectValue); langSelectValue = null } }}>+</button>
              </div>
            </div>

            <div class="post-field">
              <div class="post-field-label">Sous-titres</div>
              <div class="chips-row">
                {#each postSubs as s, i}
                  <span class="chip" class:chip-unknown={s.id === 0}>{s.name}{#if s.id === 0} ⚠{/if}<button class="chip-x" on:click={() => removeSubAt(i)}>✕</button></span>
                {/each}
                {#if !postSubs.length}<span class="chips-empty">Aucun détecté</span>{/if}
              </div>
              <div class="add-row">
                <select bind:value={subSelectValue}>
                  <option value={null}>— Ajouter un sous-titre —</option>
                  {#each subOptions.filter(o => !postSubs.find(s => s.id === o.id)) as o}
                    <option value={o}>{o.name}</option>
                  {/each}
                </select>
                <button class="btn-add" on:click={() => { if (subSelectValue) { addSub(subSelectValue); subSelectValue = null } }}>+</button>
              </div>
            </div>

            <div class="post-field">
              <div class="post-field-label">Uploader via</div>
              <div class="upload-pills">
                <button type="button"
                  class="upload-pill"
                  class:active={postUploadTypes.nzb}
                  data-color="purple"
                  on:click={() => postUploadTypes = { ...postUploadTypes, nzb: !postUploadTypes.nzb }}>
                  <span class="pill-icon"><svg class="pill-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 7.5h1.5m-1.5 3h1.5m-7.5 3h7.5m-7.5 3h7.5m3-9h3.375c.621 0 1.125.504 1.125 1.125V18a2.25 2.25 0 0 1-2.25 2.25M16.5 7.5V18a2.25 2.25 0 0 0 2.25 2.25M16.5 7.5V4.875c0-.621-.504-1.125-1.125-1.125H4.125C3.504 3.75 3 4.254 3 4.875V18a2.25 2.25 0 0 0 2.25 2.25h13.5M6 7.5h3v3H6v-3Z"/></svg></span><span class="pill-label">NZB</span>
                </button>
                <button type="button"
                  class="upload-pill"
                  class:active={postUploadTypes.ddl}
                  data-color="green"
                  on:click={() => postUploadTypes = { ...postUploadTypes, ddl: !postUploadTypes.ddl }}>
                  <span class="pill-icon"><svg class="pill-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M5.25 3.75h13.5v16.5H5.25zM8.25 3.75v5.25h7.5V3.75M9 15.75h6"/></svg></span><span class="pill-label">DDL</span>
                </button>
              </div>
              {#if postUploadTypes.ddl && comptes1F.length > 1}
                <div class="compte1f">
                  <span class="compte1f-k">Compte</span>
                  <select bind:value={compte1FActif} on:change={e => changerCompte1F(e.target.value)}>
                    {#each comptes1F as c}<option value={c}>{c}</option>{/each}
                  </select>
                </div>
              {/if}
            </div>

            <div></div><!-- Send.now retiré 2026-09-04 : 1Fichier unique host DDL -->
          </div><!-- /.post-grid -->

          <!-- Ancre pour auto-scroll vers les barres de progression -->
          <div id="progress-anchor"></div>

          <div class="post-live">
            <div class="post-live-bars">

          <!-- Barres DDL juste sous les checkboxes -->
          {#if postUploadTypes.ddl && Object.values(ddlHosts).some(h => h.active || h.done || h.error || h.posting)}
            <div class="ddl-bars">
              {#each Object.entries(ddlHosts) as [host, h]}
                {#if h.active || h.done || h.error || h.posting || h.posted}
                  <div class="ddl-bar-card" class:done={!!h.posted} class:error={!!h.error}>
                    <div class="ddl-bar-header">
                      <span class="ddl-bar-host">{host}</span>
                      {#if h.skipped}
                        <span class="ddl-bar-status skipped">⏭ Skippé</span>
                      {:else if h.error}
                        <span class="ddl-bar-status err">✗ Erreur</span>
                      {:else if h.posted}
                        <span class="ddl-bar-status ok">✓ Posté sur Elysium{#if h.hydrackerID} #{h.hydrackerID}{/if}</span>
                      {:else if h.posting}
                        <span class="ddl-bar-status posting">⬆ Post Elysium…</span>
                      {:else if h.done}
                        <span class="ddl-bar-status ok">✓ Upload terminé</span>
                      {:else}
                        <span class="ddl-bar-speed">{h.speed}</span>
                      {/if}
                      {#if h.active && !h.done && !h.posted && !h.error}
                        <button class="btn-skip-host" title="Skipper cet hébergeur (trop lent) — passe à l'épisode suivant après l'autre hébergeur"
                          on:click={async () => { try { await CancelDDLHost(host) } catch(e) {} }}>
                          ✕ Skip
                        </button>
                      {/if}
                    </div>
                    <div class="ddl-bar-filename">{h.filename}</div>
                    <div class="ddl-bar-row">
                      <div class="progress-bar" style="flex:1">
                        <div class="progress-fill"
                             class:posting={h.posting && !h.posted}
                             class:hydone={!!h.posted}
                             style="width:{h.posted || h.posting || h.done ? 100 : h.pct}%"></div>
                      </div>
                      <span class="ddl-bar-pct">{h.posted || h.posting || h.done ? '100' : h.pct.toFixed(0)}%</span>
                    </div>
                    {#if h.error}<div class="ddl-bar-errmsg">{h.error}</div>{/if}
                  </div>
                {/if}
              {/each}
            </div>
          {/if}

          <!-- Barres NZB (à l'intérieur des post-options) -->
          {#if postUploadTypes.nzb && nzbStatus}
            <div class="ddl-bars">
              <div class="ddl-bar-card" class:done={nzbStatus === 'Terminé'}>
                <div class="ddl-bar-header">
                  <span class="ddl-bar-host">NZB</span>
                  <span class="ddl-bar-speed">{nzbStatus}</span>
                </div>
                {#if nzbParparPct > 0 && nzbParparPct < 100}
                  <div class="ddl-step">
                    <div class="ddl-step-label">
                      <span>1. PAR2</span>
                      <span class="ddl-bar-speed">{nzbParparPct.toFixed(0)}%</span>
                    </div>
                    <div class="progress-bar"><div class="progress-fill" style="width:{nzbParparPct}%"></div></div>
                  </div>
                {/if}
                {#if nzbNyuuPct > 0}
                  <div class="ddl-step">
                    <div class="ddl-step-label">
                      <span>2. Usenet</span>
                      <span class="ddl-bar-speed">
                        {#if nzbNyuuArticles}{nzbNyuuArticles} · {/if}
                        {#if nzbNyuuSpeed}{nzbNyuuSpeed} · {/if}
                        {#if nzbNyuuETA}ETA {nzbNyuuETA} · {/if}
                        {nzbNyuuPct.toFixed(0)}%
                      </span>
                    </div>
                    <div class="progress-bar"><div class="progress-fill" class:hydone={nzbNyuuPct >= 100} style="width:{nzbNyuuPct}%"></div></div>
                  </div>
                {/if}
              </div>
            </div>
          {/if}
            </div><!-- /.post-live-bars -->

            {#if postEnCours}
              <aside class="post-panel">
                <div class="post-panel-title">Déroulé du post</div>
                <ol class="pp-steps">
                  {#each postSteps as st}
                    <li class="pp-step pp-{st.e}">
                      <span class="pp-dot"></span>
                      <span class="pp-label">{st.t}</span>
                      {#if st.pct !== undefined && st.e === 'now'}
                        <span class="pp-pct mono">{st.pct.toFixed(0)} %</span>
                      {/if}
                    </li>
                  {/each}
                </ol>

                <div class="pp-sep"></div>

                <div class="pp-figures">
                  <div><span class="pp-k">Écoulé</span><b class="mono">{fmtDuree(postElapsed)}</b></div>
                  <div><span class="pp-k">Restant</span><b class="mono">{resteEstime}</b></div>
                  <div><span class="pp-k">Débit</span><b class="mono">{debitTotal.toFixed(1)} Mo/s</b></div>
                  {#if volumeTotal > 0}
                    <div><span class="pp-k">Envoyé</span><b class="mono">{fmtGo(volumeEnvoye)} / {fmtGo(volumeTotal)}</b></div>
                  {/if}
                </div>

                {#if debitSeries.length > 1}
                  <svg class="pp-courbe" viewBox="0 0 {COURBE_W} {COURBE_H}" preserveAspectRatio="none" aria-hidden="true">
                    {#if courbeDdl}<polyline class="pp-l-ddl" points={courbeDdl} />{/if}
                    {#if courbeNzb}<polyline class="pp-l-nzb" points={courbeNzb} />{/if}
                  </svg>
                  <div class="pp-legende">
                    <span class="pp-lg pp-lg-ddl">1Fichier {ddlDebit.toFixed(1)}</span>
                    <span class="pp-lg pp-lg-nzb">Usenet {nzbDebit.toFixed(1)}</span>
                    <span class="pp-lg-max mono">max {debitMax.toFixed(0)} Mo/s</span>
                  </div>
                {/if}

                {#if queue.length > 0}
                  <div class="pp-sep"></div>
                  <div class="pp-next">
                    <span class="pp-k">Ensuite</span>
                    <b>{(queue[0] || '').split(/[\\/]/).pop()}</b>
                    <span class="pp-reste mono">+{queue.length - 1 > 0 ? queue.length - 1 : 0}</span>
                  </div>
                {/if}
              </aside>
            {/if}
          </div><!-- /.post-live -->

        </div>

        <!-- Résultat -->
        {#if postResult}
          <div class="post-result" class:post-result-ok={postResult.ok} class:post-result-err={!postResult.ok}>
            {postResult.ok ? '✓' : '✗'} {postResult.message}
            {#if postResult.details}<div class="post-result-details">{postResult.details}</div>{/if}

            {#if postResult.queue && queueResults.length}
              {@const echecs = queueResults.filter(r => !r.ok)}
              {@const reussites = queueResults.filter(r => r.ok)}
              <div class="qr">
                {#if echecs.length}
                  <div class="qr-head">{echecs.length} échec{echecs.length > 1 ? 's' : ''}</div>
                  {#each echecs as r}
                    <div class="qr-row">
                      <div class="qr-name" title={r.filename}>{r.filename}</div>
                      <div class="qr-msg">{resumeErreur(r.message)}</div>
                      <details class="qr-det">
                        <summary>message complet</summary>
                        <pre>{r.message}</pre>
                      </details>
                    </div>
                  {/each}
                {/if}
                {#if reussites.length}
                  <button type="button" class="qr-toggle" on:click={() => queueOkOpen = !queueOkOpen}>
                    {queueOkOpen ? '▾' : '▸'} {reussites.length} réussite{reussites.length > 1 ? 's' : ''}
                  </button>
                  {#if queueOkOpen}
                    <div class="qr-ok-list">
                      {#each reussites as r}
                        <div class="qr-ok" title={r.message}>{r.filename}</div>
                      {/each}
                    </div>
                  {/if}
                {/if}
              </div>
            {/if}
          </div>
        {/if}


        {#if file}
          <div class="nfo-preview" class:open={nfoOpen}>
            <button type="button" class="nfo-preview-header" on:click={() => nfoOpen = !nfoOpen}>
              <span class="mi-chevron">{nfoOpen ? '▾' : '▸'}</span>
              <span><svg class="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M14.25 3.75H6.75A1.5 1.5 0 0 0 5.25 5.25v13.5a1.5 1.5 0 0 0 1.5 1.5h10.5a1.5 1.5 0 0 0 1.5-1.5V8.25l-4.5-4.5ZM14.25 3.75V8.25h4.5"/></svg>NFO (inclus dans NZB + DDL + Torrent){postNfoManual.trim() ? ' — édité' : ''}</span>
              <button class="btn-copy" title={nfoEditMode ? 'Mode preview' : 'Mode édition'} on:click|stopPropagation={() => nfoEditMode = !nfoEditMode}>{@html nfoEditMode ? EYE_SVG : PEN_SVG}</button>
              <button class="btn-copy" title="Copier" on:click|stopPropagation={() => navigator.clipboard.writeText(nfoPreview)}>{@html COPY_SVG}</button>
            </button>
            {#if nfoOpen}
              {#if nfoEditMode}
                <textarea class="nfo-edit" bind:value={postNfoManual}
                  placeholder={(file && (mediaInfo || fileInfo)) ? 'Vide = utilise le NFO auto-généré ci-dessous. Tape ton NFO custom ici pour overrider.' : 'Tape ton NFO custom ici (saison complète : pas de MediaInfo auto).'}
                  rows="14"></textarea>
                {#if !postNfoManual.trim() && nfoPreview}
                  <pre class="nfo-preview-body" style="opacity:0.5;border-top:1px dashed var(--border);margin-top:6px"><span style="color:var(--text3);font-size:10px">↓ NFO auto (sera utilisé si tu laisses vide)</span>
{nfoPreview}</pre>
                {/if}
              {:else if nfoPreview}
                <pre class="nfo-preview-body">{nfoPreview}</pre>
              {:else}
                <div class="nfo-preview-body" style="color:var(--text3);font-style:italic">— Pas de NFO généré (clique ✏️ pour en saisir un manuellement)</div>
              {/if}
            {/if}
          </div>
        {/if}
      {/if}

    </div>
  </div>
  {/if}
</div>

<style>  .hydracker-tab { height: 100%; overflow-y: auto; padding: 24px; }

  .queue-bar {
    margin-bottom: 18px;
    background: linear-gradient(180deg, rgba(255, 107, 26, 0.07) 0%, rgba(255, 107, 26, 0.02) 100%);
    border: 1px solid rgba(255, 107, 26, 0.24);
    border-radius: 12px;
    padding: 12px 14px;
  }
  .queue-head { display: flex; align-items: center; gap: 12px; }
  .queue-title { font-family: var(--font-display); font-size: 11px; font-weight: 700;
    color: var(--ember); text-transform: uppercase; letter-spacing: .18em; }
  .queue-counter { font-size: 11px; color: var(--text2); flex: 1; }
  .release-current {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 14px; margin-bottom: 8px;
    background: linear-gradient(90deg, rgba(255,107,26,0.09), transparent 45%);
    border: 1px solid rgba(255,107,26,0.26);
    border-radius: 10px;
  }
  /* Avec l'épine, le bloc passe sur deux lignes : grille au lieu de flex. */
  /* align-items:center + hauteur fixe de l'épine : sinon les 4 phases
     s'étirent et imposent la hauteur du bloc (~95 px pour deux lignes). */
  .release-current.has-spine {
    display: grid; grid-template-columns: 26px 1fr; gap: 0;
    align-items: center; padding: 9px 14px 9px 0;
  }
  .release-body { min-width: 0; }
  .release-line { display: flex; align-items: center; gap: 10px; min-width: 0; }

  /* ── Épine dorsale : les 4 phases, de haut en bas ── */
  /* Hauteur NATURELLE : ni `height` fixe ni `flex:1` sur les phases.
     Avec une hauteur fixe l'épine débordait du bloc, et avec `flex:1`
     elle s'étirait et le gonflait. Segments courts et non extensibles :
     l'épine fait ~58 px, le bloc se dimensionne dessus. */
  .spine {
    display: flex; flex-direction: column; align-items: center;
    justify-content: center;
    border-right: 1px solid rgba(255,107,26,0.18);
  }
  .ph { display: flex; flex-direction: column; align-items: center; }
  .ph-dot {
    width: 7px; height: 7px; border-radius: 50%; flex: none;
    border: 1.5px solid var(--text3); background: var(--bg);
  }
  .ph-stem { width: 1.5px; height: 6px; flex: none; background: var(--border-strong); margin: 2px 0; }
  .ph.done .ph-dot { background: var(--success); border-color: var(--success); }
  .ph.done .ph-stem { background: var(--success); opacity: .5; }
  .ph.now .ph-dot {
    background: var(--ember); border-color: var(--ember);
    box-shadow: 0 0 0 3px rgba(255,107,26,.18), 0 0 10px var(--accent-glow);
  }

  /* Rappel textuel des phases sous le nom de la release. */
  .release-phases {
    display: flex; align-items: center; gap: 5px; margin-top: 3px;
    font-family: var(--font-mono); font-size: 9.5px; letter-spacing: .06em;
  }
  .phl { color: var(--text3); }
  .phl.done { color: var(--success); }
  .phl.now { color: var(--ember); font-weight: 700; }
  .phsep { color: var(--text3); opacity: .4; }

  .release-current-label {
    font-size: 10.5px; font-weight: 700; color: var(--ember-hi);
    font-family: var(--font-display); letter-spacing: .13em; flex: none;
  }
  .release-current-name {
    font-family: ui-monospace, Menlo, monospace; font-size: 11px;
    color: #7ef0c0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    min-width: 0; flex: 1;
  }
  .btn-stop-inline {
    background: #dc2626; color: white; border: 0;
    padding: 5px 12px; font-size: 11px; font-weight: 700;
    letter-spacing: 0.5px; border-radius: 6px; cursor: pointer; flex: none;
  }
  .btn-stop-inline:hover { background: #ef4444; }
  .btn-skip-ep {
    background: rgba(255,214,10,0.15); color: #ffe066;
    border: 1px solid rgba(255,214,10,0.4);
    padding: 5px 10px; font-size: 11px; font-weight: 600;
    border-radius: 6px; cursor: pointer; flex: none;
  }
  .btn-skip-ep:hover { background: rgba(255,214,10,0.25); }
  .btn-skip-host {
    background: transparent; color: var(--text3);
    border: 1px solid rgba(255,255,255,0.1);
    padding: 2px 7px; font-size: 10px; font-weight: 600;
    border-radius: 5px; cursor: pointer; flex: none;
  }
  .btn-skip-host:hover { background: rgba(239,68,68,0.15); color: #ef4444; border-color: rgba(239,68,68,0.3); }
  .queue-list {
    display: grid; grid-template-columns: 1fr 1fr; gap: 4px 8px; margin-top: 8px;
    max-height: 100px;   /* ~3 lignes × 2 cols = 6 items visibles */
    overflow-y: auto;
    padding-right: 4px;
  }
  .queue-list::-webkit-scrollbar { width: 6px; }
  .queue-list::-webkit-scrollbar-track { background: rgba(255,255,255,0.02); border-radius: 3px; }
  .queue-list::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.12); border-radius: 3px; }
  .queue-list::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.2); }
  .queue-item { display: flex; align-items: center; gap: 8px; padding: 4px 8px; background: rgba(0,0,0,0.2); border-radius: 6px; min-width: 0; }
  .queue-item .queue-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; }
  @media (max-width: 900px) { .queue-list { grid-template-columns: 1fr; max-height: 200px; } }
  .queue-idx { font-size: 10px; color: var(--text3); width: 30px; flex: none; font-variant-numeric: tabular-nums; }
  .queue-name { flex: 1; font-family: ui-monospace, Menlo, monospace; font-size: 11px; color: var(--text2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  /* Drop zone */
  .dropzone {
    border: 2px dashed var(--border-strong); border-radius: 16px;
    padding: 60px 40px; text-align: center; cursor: pointer;
    transition: all 200ms ease; color: var(--text3);
    background: linear-gradient(180deg, rgba(255,255,255,0.02) 0%, rgba(255,255,255,0.005) 100%);
  }
  .dropzone.drag {
    border-color: rgba(255, 107, 26, 0.55);
    background: rgba(255, 107, 26, 0.06);
    color: var(--blue-hot);
  }
  .drop-icon { font-size: 48px; margin-bottom: 12px; }
  .dropzone p { margin-bottom: 6px; font-size: 15px; color: var(--text2); }
  .drop-sub { font-size: 12px; color: var(--text3); }
  .drop-buttons { display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; margin-top: 8px; }
  .multi-modal-bg {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.6);
    backdrop-filter: blur(4px);
    z-index: 1000;
    display: flex; justify-content: center; align-items: center;
  }
  .multi-modal {
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 24px;
    max-width: 520px;
    width: 90%;
    box-shadow: 0 20px 60px rgba(0,0,0,0.5);
  }
  .multi-modal-title { font-size: 16px; font-weight: 700; color: var(--text); margin-bottom: 4px; }
  .multi-modal-msg { font-size: 13px; color: var(--text3); margin-bottom: 16px; }
  .multi-modal-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 12px; }
  .multi-btn {
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 18px 12px;
    cursor: pointer;
    color: var(--text);
    transition: all 0.15s;
    text-align: center;
  }
  .multi-btn:hover { border-color: var(--blue-hot); background: rgba(96, 165, 250, 0.08); transform: translateY(-1px); }
  .multi-btn-emoji { font-size: 28px; margin-bottom: 6px; }
  .multi-btn-label { font-weight: 700; font-size: 14px; }
  .multi-btn-hint { font-size: 11px; color: var(--text3); margin-top: 4px; }
  .multi-btn-saison:hover { border-color: #c4b5fd; background: rgba(168, 85, 247, 0.08); }
  .multi-cancel {
    width: 100%;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 8px;
    color: var(--text3);
    cursor: pointer;
    font-size: 12px;
  }
  .multi-cancel:hover { color: var(--text); border-color: var(--text3); }  .btn-browse {
    display: inline-block; margin-top: 14px;
    color: #fff; background: var(--grad-primary);
    border: 1px solid rgba(0,0,0,0.25);
    padding: 10px 22px; border-radius: 10px;
    font-size: 13px; font-weight: 600; letter-spacing: 0.2px;
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.25),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 1px 2px rgba(0,0,0,0.4),
      0 8px 24px -6px var(--accent-glow);
  }
  .btn-browse:hover { background: var(--grad-primary-hover); filter: brightness(1.05); }
  .btn-browse:active { transform: translateY(1px); }

  /* Main grid — layout 2 colonnes pour éviter de scroller */
  /* Colonne d'infos (fiche / média / récap) à DROITE, paramètres à
     gauche — disposition de la maquette. L'inversion se fait par `order`
     plutôt qu'en déplaçant le balisage : col-left reste col-left dans le
     DOM, seul son rendu bascule. */

  /* ── Fond d'ambiance ─────────────────────────────────────────────── */
  .ambiance {
    position: fixed; inset: -60px; z-index: -1; pointer-events: none;
    background-size: cover; background-position: center top;
    filter: blur(58px) saturate(1.25);
    opacity: .22;
    /* fondu vers le bas pour ne pas concurrencer le contenu */
    -webkit-mask-image: linear-gradient(180deg, #000 0%, rgba(0,0,0,.45) 38%, transparent 72%);
            mask-image: linear-gradient(180deg, #000 0%, rgba(0,0,0,.45) 38%, transparent 72%);
  }

  /* ── Apparition en cascade ───────────────────────────────────────────
     Uniquement transform + opacity : composé par la carte graphique, donc
     aucune repeinte. Animation ponctuelle au chargement, jamais en boucle. */
  @keyframes gpt-reveal {
    from { opacity: 0; transform: translateY(7px); }
    to   { opacity: 1; transform: none; }
  }
  .reveal > * { animation: gpt-reveal .34s cubic-bezier(.22,.8,.3,1) both; }
  .reveal > :nth-child(1) { animation-delay: .02s; }
  .reveal > :nth-child(2) { animation-delay: .07s; }
  @media (prefers-reduced-motion: reduce) {
    .reveal > * { animation: none; }
  }
  .main-grid { display: grid; grid-template-columns: 1fr 340px; gap: 18px; align-items: start; }
  .col-left  { order: 2; }
  .col-right { order: 1; min-width: 0; }
  @media (max-width: 1100px) {
    .main-grid { grid-template-columns: 1fr; }
  }

  /* Left col */
  .col-left { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  /* L'affiche est plus étroite que la colonne : on la centre plutôt que
     de la laisser collée au bord gauche. */
  .poster-wrap { margin-left: auto; margin-right: auto; }
  .fiche-title, .fiche-year { text-align: center; }
  .poster-wrap {
    /* 104 px et non 260 : en 2/3, une affiche de 260 px faisait 390 px
       de haut et repoussait le média et le récapitulatif hors de l'écran. */
    width: 104px; aspect-ratio: 2/3; border-radius: 8px; overflow: hidden;
    background: var(--bg2);
    border: 1px solid var(--border);
    box-shadow: 0 6px 24px -8px rgba(0,0,0,0.6);
  }
  .poster-wrap img { width: 100%; height: 100%; object-fit: cover; }
  .poster-placeholder {
    width: 100%; height: 100%;
    display: flex; align-items: center; justify-content: center;
    font-size: 60px; color: var(--text3);
  }
  .fiche-title { font-weight: 700; font-size: 14px; color: var(--text); letter-spacing: -0.01em; }
  .fiche-year { font-size: 12px; color: var(--text3); }
  .btn-ghost-sm {
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    color: var(--text2);
    padding: 5px 11px; font-size: 11px; border-radius: 7px;
    text-transform: uppercase; letter-spacing: 0.4px;
    transition: all 160ms ease;
  }
  .btn-ghost-sm:hover {
    color: var(--text);
    background: rgba(255, 107, 26, 0.08);
    border-color: rgba(255, 107, 26, 0.35);
  }

  .mi-loading { font-size: 12px; color: var(--text3); }
  .mi-error {
    font-size: 12px; color: #f4c17a;
    background: rgba(234, 179, 8, 0.08);
    border: 1px solid rgba(234, 179, 8, 0.25);
    border-radius: 8px; padding: 10px 12px; line-height: 1.45;
  }
  .mi-error strong { color: #ffd97a; }
  .mi-error-detail { margin-top: 6px; font-size: 10.5px; color: var(--text3); opacity: 0.75; }
  .mi-details { background: var(--bg2); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
  .mi-summary {
    cursor: pointer; padding: 8px 12px; font-size: 11px; color: var(--text2);
    background: transparent; border: 0; width: 100%; text-align: left;
    display: flex; align-items: center; gap: 6px; user-select: none;
  }
  .mi-summary:hover { background: rgba(255,255,255,0.03); }
  .mi-chevron { color: var(--text3); font-size: 10px; display: inline-block; width: 10px; }
  .mi-details.open .mi-summary { border-bottom: 1px solid var(--border); }
  .mi-details .mi-block { padding: 8px 12px; }

  .mi-block {
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px solid var(--border);
    border-radius: 12px; padding: 13px; font-size: 11px;
  }  .mi-row {
    display: flex; justify-content: space-between;
    padding: 4px 0; border-bottom: 1px solid rgba(255,255,255,0.05);
    color: var(--text3);
  }
  .mi-row:last-child { border-bottom: none; }
  .mi-row span:last-child { color: var(--text); text-align: right; max-width: 120px; }

  .tracks-block { margin-top: 8px; padding-top: 10px; border-top: 1px solid var(--border); }
  .tracks-header { font-family: var(--font-display); font-size: 9.5px; color: var(--text3);
    margin-bottom: 8px; font-weight: 700; letter-spacing: .18em; text-transform: uppercase; }
  .track-row {
    display: grid;
    grid-template-columns: 70px 1fr auto auto;
    gap: 8px;
    align-items: center;
    padding: 4px 0;
    font-size: 12px;
    border-bottom: 1px dashed rgba(255,255,255,0.04);
  }
  .track-row:last-child { border-bottom: none; }
  .track-row-muted { opacity: 0.5; }
  .track-badge {
    font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; text-align: center;
  }
  .track-badge-audio { background: rgba(96, 165, 250, 0.15); color: #93c5fd; }
  .track-badge-sub { background: rgba(168, 85, 247, 0.15); color: #c4b5fd; }
  .track-title { color: var(--text); font-family: ui-monospace, monospace; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .track-arrow { color: var(--text3); }
  .track-mapped { color: #7ef0c0; font-weight: 600; }
  .track-mapped-warn { color: #fbbf24; }

  /* Right col */
  .col-right { display: flex; flex-direction: column; gap: 14px; }

  .file-badge {
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 9px 13px; font-size: 12px; color: var(--text2);
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    word-break: break-all;
  }
  .btn-x {
    background: transparent; color: var(--text3); font-size: 14px;
    padding: 2px 7px; border-radius: 6px;
    transition: all 140ms ease;
  }
  .btn-x:hover { color: var(--red-hot); background: rgba(239, 68, 68, 0.08); }

  .parser-row { display: flex; flex-wrap: wrap; gap: 6px; }
  .tag {
    background: rgba(255,255,255,0.05);
    border: 1px solid var(--border);
    border-radius: 6px; padding: 3px 9px; font-size: 11px; color: var(--text2);
  }
  .tag-title { background: rgba(0, 180, 216, 0.12); color: var(--blue-hot); font-weight: 600; border-color: rgba(0, 180, 216, 0.25); }
  .tag-qual { background: rgba(230, 57, 70, 0.12); color: var(--red-hot); border-color: rgba(230, 57, 70, 0.25); }
  .tag-codec { background: rgba(34, 197, 94, 0.12); color: #7ef0c0; border-color: rgba(34, 197, 94, 0.25); }
  .tag-lang { background: rgba(247, 127, 0, 0.12); color: var(--orange); border-color: rgba(247, 127, 0, 0.25); }

  /* Ambiguïté */
  .ambig-box {
    background: rgba(255, 214, 10, 0.05);
    border: 1px solid rgba(255, 214, 10, 0.25);
    border-radius: 12px; padding: 14px;
  }
  .ambig-title { font-size: 12px; color: var(--yellow); margin-bottom: 10px;   display: flex; align-items: center; gap: 7px;
  }
  .ambig-list { display: flex; flex-direction: column; gap: 6px; max-height: 220px; overflow-y: auto; }
  .ambig-item {
    display: flex; align-items: center; gap: 10px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 8px; padding: 7px 11px; text-align: left;
    transition: all 160ms ease;
  }
  .ambig-item:hover {
    background: rgba(255,255,255,0.06);
    border-color: rgba(255, 107, 26, 0.35);
  }
  .ambig-item img { width: 36px; height: 54px; object-fit: cover; border-radius: 4px; }
  .ambig-no-poster {
    width: 36px; height: 54px;
    background: var(--bg2); border-radius: 4px;
    display: flex; align-items: center; justify-content: center; font-size: 18px;
  }
  .ambig-name { font-size: 13px; font-weight: 500; color: var(--text); }
  .ambig-year { font-size: 11px; color: var(--text3); }
  .search-section {
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px solid var(--border);
    border-radius: 12px; padding: 14px;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.05);
  }
  /* Étiquette de section : Oxanium très espacé + filet qui occupe
     la largeur restante, pour poser une ligne de lecture nette. */
  .search-label, .post-label {
    display: flex; align-items: center; gap: 9px;
    font-family: var(--font-display);
    font-size: 9.5px; font-weight: 700; color: var(--text3);
    letter-spacing: .2em; text-transform: uppercase;
    margin-bottom: 11px;
  }
  .search-label::after, .post-label::after {
    content: ""; flex: 1; height: 1px; background: var(--border);
  }
  .search-msg {
    display: flex; align-items: center; gap: 8px;
    margin-top: 9px; padding: 8px 11px;
    background: rgba(255,200,61,.07);
    border: 1px solid rgba(255,200,61,.3);
    border-radius: 8px;
    font-size: 11.5px; color: var(--warning);
  }
  .search-msg span { flex: 1; min-width: 0; }
  .search-msg-x {
    background: none; border: none; color: var(--warning);
    cursor: pointer; font-size: 12px; padding: 0 2px; opacity: .7;
  }
  .search-msg-x:hover { opacity: 1; }
  .search-row { display: flex; gap: 8px; }
  .search-row input { flex: 1; }
  .btn-search {
    color: #fff; background: var(--grad-primary);
    border: 1px solid rgba(0,0,0,0.25);
    padding: 8px 18px; flex: none; font-weight: 600;
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.25),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 1px 2px rgba(0,0,0,0.4),
      0 6px 18px -6px var(--accent-glow);
  }
  .btn-search:hover:not(:disabled) { background: var(--grad-primary-hover); filter: brightness(1.05); }
  .btn-search:disabled { opacity: 0.5; cursor: default; box-shadow: none; }

  .tmdb-type-toggle {
    display: inline-flex;
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 2px;
    gap: 2px;
    flex: none;
  }
  /* ⚠ Tailles OBLIGATOIRES : un <svg> sans width/height remplit son
     conteneur. Sans ces règles, le picto de la zone de dépôt s'étalait
     sur tout l'écran (constaté en v9.4.0). */
  .ui-ico   { width: 14px; height: 14px; flex: none; vertical-align: -2px; }
  .drop-ico { width: 48px; height: 48px; opacity: .55; }
  .ph-ico   { width: 28px; height: 28px; opacity: .4; }
  .pill-ico { width: 13px; height: 13px; flex: none; }
  .tt-icon { width: 14px; height: 14px; flex: none; }
  .tmdb-type-toggle button {
    display: inline-flex; align-items: center; gap: 6px;
    background: transparent;
    border: none;
    color: var(--text3);
    padding: 5px 11px;
    font-family: var(--font-display); font-weight: 600; letter-spacing: .05em;
    font-size: 11.5px;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .tmdb-type-toggle button:hover { color: var(--text); }
  .tmdb-type-toggle button.active {
    background: rgba(255, 107, 26, 0.13);
    color: var(--ember-hi);
  }

  .hydracker-results { margin-top: 10px; display: flex; flex-direction: column; gap: 4px; max-height: 200px; overflow-y: auto; }
  .hydracker-item {
    display: flex; align-items: center; gap: 10px;
    background: rgba(255,255,255,0.03);
    border: 1px solid var(--border);
    border-radius: 8px; padding: 8px 11px; text-align: left;
    transition: all 160ms ease;
  }
  .hydracker-item:hover {
    background: rgba(255,255,255,0.06);
    border-color: rgba(255, 107, 26, 0.35);
  }  .hyd-name { flex: 1; font-size: 13px; color: var(--text); }
  .hyd-year { font-size: 11px; color: var(--text3); }
  .hyd-type { font-size: 10px; padding: 2px 7px; border-radius: 9999px; margin-left: 6px; font-weight: 500; }
  .badge-movie { background: rgba(0, 180, 216, 0.15); color: var(--blue-hot); }
  .badge-series { background: rgba(34, 197, 94, 0.15); color: #7ef0c0; }
  .selected-hyd {
    font-size: 12px; color: #7ef0c0; margin-top: 8px;
    display: flex; align-items: center; gap: 10px;
    background: rgba(34, 197, 94, 0.08);
    border: 1px solid rgba(34, 197, 94, 0.3);
    border-radius: 10px; padding: 9px 11px;
  }
  .hyd-poster { width: 40px; height: 60px; object-fit: cover; border-radius: 5px; flex: none; }
  .hyd-poster-placeholder {
    background: var(--bg2);
    display: flex; align-items: center; justify-content: center;
    font-size: 18px; color: var(--text3);
  }
  .hyd-info { display: flex; flex-direction: column; gap: 3px; }
  .hyd-info-name { font-weight: 600; color: #7ef0c0; font-size: 13px; }
  .hyd-info-meta { font-size: 11px; color: var(--text3); display: flex; align-items: center; gap: 4px; }
  .hyd-create-box {
    margin-top: 8px;
    background: rgba(255, 214, 10, 0.05);
    border: 1px solid rgba(255, 214, 10, 0.25);
    border-radius: 12px; padding: 14px;
    display: flex; flex-direction: column; gap: 8px;
  }
  .hyd-create-title { font-size: 13px; font-weight: 600; color: var(--yellow); }
  .hyd-create-hint { font-size: 11px; color: var(--text2); }
  .hyd-manual-error { font-size: 11px; color: #ff6b6b; margin-top: 4px; }
  .hyd-tmdb-url-row {
    display: flex; align-items: center; gap: 6px;
    background: rgba(0,0,0,0.25);
    border-radius: 6px; padding: 6px 9px;
  }
  .hyd-tmdb-url { font-size: 11px; color: var(--blue-hot); word-break: break-all; flex: 1; }
  .hyd-tmdb-input {
    flex: 1;
    background: rgba(255,255,255,0.05);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--blue-hot);
    font-size: 13px;
    font-weight: 600;
    padding: 6px 10px;
    font-family: ui-monospace, monospace;
  }
  .hyd-tmdb-input:focus { outline: none; border-color: var(--blue-hot); }
  .btn-copy {
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    color: var(--text2); border-radius: 5px; padding: 3px 7px;
    cursor: pointer; font-size: 13px; flex: none;
  }
  .btn-copy:hover { background: rgba(255,255,255,0.08); color: var(--text); }  .hyd-id-row { display: flex; gap: 6px; }
  .hyd-type-toggle {
    display: flex; gap: 6px; margin: 6px 0 8px;
  }
  .hyd-type-toggle label {
    flex: 1;
    display: flex; align-items: center; justify-content: center; gap: 4px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: 8px;
    font-size: 12px; font-weight: 500;
    color: var(--text2);
    cursor: pointer;
    transition: all 0.15s ease;
    user-select: none;
  }
  .hyd-type-toggle label input { display: none; }
  .hyd-type-toggle label:hover { background: rgba(255,255,255,0.04); }
  .hyd-type-toggle label.active {
    background: rgba(126,240,192,0.10);
    border-color: rgba(126,240,192,0.5);
    color: var(--text);
  }

  /* Post options */
  .post-options {
    background: linear-gradient(180deg, rgba(255,255,255,0.035) 0%, rgba(255,255,255,0.015) 100%);
    border: 1px solid var(--border);
    border-radius: 14px; padding: 16px 18px;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.05), 0 1px 2px rgba(0,0,0,0.4);
  }
  /* Style unifié avec .search-label, déclaré plus haut (section
     « Panneaux d'instrument ») : ne rien remettre ici, cette règle
     étant plus basse dans la feuille elle gagnerait. */
  .post-label { margin-bottom: 14px; }
  .post-field { margin-bottom: 14px; }
  .post-field label, .post-field-label {
    display: block; font-size: 11px; color: var(--text3);
    margin-bottom: 6px; text-transform: uppercase; letter-spacing: 0.6px;
  }
  .post-field select { width: 100%; }

  .se-row { display: flex; gap: 10px; }
  .se-label { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--text2); }
  .se-input { width: 70px; padding: 4px 8px; font-size: 12px; }
  .se-input:disabled { opacity: 0.4; cursor: not-allowed; }
  .full-saison-toggle {
    display: flex; align-items: center; gap: 6px;
    margin-top: 6px;
    font-size: 12px;
    color: var(--text2);
    cursor: pointer;
    user-select: none;
  }
  .full-saison-toggle input[type="checkbox"] { cursor: pointer; }
  .full-saison-toggle:has(input:checked) > span:first-of-type { color: #93c5fd; font-weight: 600; }
  .full-saison-hint { color: var(--text3); font-size: 11px; font-style: italic; }

  .chips-row { display: flex; flex-wrap: wrap; gap: 5px; margin-bottom: 4px; min-height: 24px; align-items: center; }
  .chips-empty { font-size: 11px; color: var(--text3); font-style: italic; }
  .chip {
    display: inline-flex; align-items: center; gap: 5px;
    background: rgba(255, 107, 26, 0.1);
    border: 1px solid rgba(255, 107, 26, 0.3);
    color: var(--blue-hot);
    padding: 4px 10px; font-size: 11px; border-radius: 7px;
    font-weight: 500;
  }
  .chip-unknown {
    background: rgba(255, 214, 10, 0.08);
    border-color: rgba(255, 214, 10, 0.35);
    color: var(--yellow);
  }
  .chip-x {
    background: none; color: inherit; opacity: 0.6;
    padding: 0 2px; font-size: 11px; line-height: 1;
    border-radius: 3px;
  }
  .chip-x:hover { color: var(--red-hot); opacity: 1; }

  .add-row { display: flex; gap: 6px; margin-top: 8px; }
  .add-row select { flex: 1; }
  .btn-add {
    background: rgba(255,255,255,0.04);
    border: 1px solid var(--border);
    color: var(--blue-hot);
    padding: 6px 14px; font-size: 14px; font-weight: 600;
    border-radius: 7px; flex: none;
    transition: all 160ms ease;
  }
  .btn-add:hover {
    background: rgba(255, 107, 26, 0.08);
    border-color: rgba(255, 107, 26, 0.35);
  }

  /* Pills modernes "Uploader via" */
  /* Sélecteur de compte 1Fichier : n'apparaît qu'avec plusieurs comptes
     configurés, sinon il n'y a rien à choisir. */
  .compte1f { display: flex; align-items: center; gap: 8px; margin-top: 9px; }
  .compte1f-k {
    font-family: var(--font-display); font-size: 9px; font-weight: 700;
    text-transform: uppercase; letter-spacing: .18em; color: var(--text3);
  }
  .compte1f select {
    padding: 5px 9px; font-size: 11.5px; border-radius: 7px;
    background: rgba(61,220,132,.07); border: 1px solid rgba(61,220,132,.3);
    color: var(--success); font-weight: 600;
  }
  .upload-pills {
    display: flex;
    gap: 8px;
    flex-wrap: nowrap;
    align-items: stretch;
    overflow-x: auto;
    padding: 2px 0;
    scrollbar-width: thin;
  }
  .upload-pills::-webkit-scrollbar { height: 4px; }
  .upload-pills::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 4px; }

  .upload-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    height: 36px;
    padding: 0 16px;
    background: rgba(255,255,255,0.03);
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 999px;
    color: var(--text2, rgba(255,255,255,0.7));
    font-size: 12.5px;
    font-weight: 600;
    letter-spacing: 0.2px;
    cursor: pointer;
    user-select: none;
    transition: all 0.18s cubic-bezier(0.4, 0, 0.2, 1);
    white-space: nowrap;
    position: relative;
  }
  .upload-pill:hover {
    background: rgba(255,255,255,0.06);
    border-color: rgba(255,255,255,0.16);
    color: var(--text, #fff);
    transform: translateY(-1px);
  }
  .upload-pill .pill-icon {
    font-size: 15px;
    line-height: 1;
    transition: transform 0.18s ease;
  }
  .upload-pill:hover .pill-icon { transform: scale(1.1); }
  .upload-pill .pill-label { line-height: 1; }

  /* État actif — couleur dynamique selon data-color */
  .upload-pill.active {
    color: #0d0a10;
    font-weight: 700;
    border-color: transparent;
  }
  .upload-pill[data-color="purple"].active {
    background: linear-gradient(135deg, #a78bfa, #8b5cf6);
    color: #fff;
    box-shadow: 0 4px 16px -4px rgba(167,139,250,0.5), inset 0 1px 0 rgba(255,255,255,0.25);
  }
  .upload-pill[data-color="green"].active {
    background: linear-gradient(135deg, #7ef0c0, #34d399);
    box-shadow: 0 4px 16px -4px rgba(126,240,192,0.5), inset 0 1px 0 rgba(255,255,255,0.3);
  }

  .post-result {
    margin-top: 10px; padding: 10px 13px;
    border-radius: 10px; font-size: 12px; font-weight: 500;
  }
  .post-result-ok {
    background: rgba(34, 197, 94, 0.08);
    border: 1px solid rgba(34, 197, 94, 0.25);
    color: #7ef0c0;
  }
  .post-result-err {
    background: rgba(239, 68, 68, 0.08);
    border: 1px solid rgba(239, 68, 68, 0.25);
    color: #ff9585;
  }
  .post-result-details { margin-top: 4px; font-size: 11px; opacity: 0.75; font-weight: 400; }

  /* Récapitulatif de queue : les échecs doivent se voir sans chercher. */
  .qr { margin-top: 10px; }
  .qr-head {
    font-family: var(--font-display); font-size: 9px; font-weight: 700;
    text-transform: uppercase; letter-spacing: .2em; color: var(--red-hot);
    margin-bottom: 7px;
  }
  .qr-row {
    border-left: 2px solid var(--danger);
    background: rgba(255,77,94,.06);
    border-radius: 0 7px 7px 0;
    padding: 8px 11px; margin-bottom: 7px;
  }
  .qr-name {
    font-size: 11.5px; font-weight: 600; color: var(--text1);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .qr-msg { font-size: 11px; color: var(--red-hot); margin-top: 3px; }
  .qr-det { margin-top: 5px; }
  .qr-det summary {
    font-size: 10px; color: var(--text3); cursor: pointer; list-style: none;
  }
  .qr-det summary::before { content: "▸ "; }
  .qr-det[open] summary::before { content: "▾ "; }
  .qr-det summary:hover { color: var(--text2); }
  .qr-det pre {
    margin: 5px 0 0; padding: 7px 9px; border-radius: 6px;
    background: rgba(0,0,0,.3); border: 1px solid var(--border);
    font-family: var(--font-mono); font-size: 10px; color: var(--text2);
    white-space: pre-wrap; max-height: 160px; overflow: auto;
  }
  .qr-toggle {
    background: none; border: none; cursor: pointer; padding: 3px 0;
    font-family: var(--font-display); font-size: 9px; font-weight: 700;
    text-transform: uppercase; letter-spacing: .2em; color: var(--success);
  }
  .qr-toggle:hover { color: var(--text1); }
  .qr-ok-list { max-height: 220px; overflow: auto; margin-top: 5px; }
  .qr-ok {
    font-size: 11px; color: var(--text3); padding: 2px 0;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .post-actions { display: flex; gap: 10px; margin-top: 16px; }
  .btn-start {
    color: #fff; background: var(--grad-primary);
    border: 1px solid rgba(0,0,0,0.25);
    padding: 11px 24px; font-weight: 600;
    letter-spacing: 0.2px; font-size: 13px;
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.25),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 1px 2px rgba(0,0,0,0.4),
      0 8px 24px -6px var(--accent-glow);
  }
  .btn-start:hover:not(:disabled) {
    background: var(--grad-primary-hover);
    filter: brightness(1.05);
    box-shadow:
      inset 0 1px 0 rgba(255,255,255,0.3),
      inset 0 -1px 0 rgba(0,0,0,0.2),
      0 2px 6px rgba(0,0,0,0.5),
      0 12px 32px -4px var(--accent-glow);
  }
  .btn-start:active:not(:disabled) { transform: translateY(1px); filter: brightness(0.95); }
  .btn-start:disabled { opacity: 0.4; cursor: default; box-shadow: none; }
  .btn-reset {
    background: rgba(255,255,255,0.03);
    color: var(--text2);
    border: 1px solid var(--border);
    padding: 10px 16px;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.04);
  }
  .btn-cancel {
    background: rgba(239, 68, 68, 0.12);
    color: #ff9585;
    border: 1px solid rgba(239, 68, 68, 0.35);
    padding: 10px 16px; font-weight: 600;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.04);
  }
  .btn-cancel:hover {
    background: rgba(239, 68, 68, 0.2);
    border-color: rgba(239, 68, 68, 0.55);
  }
  .btn-reset:hover {
    background: rgba(255,255,255,0.06);
    border-color: rgba(255, 107, 26, 0.35);
    color: var(--text);
  }
  /* Recherches TMDB + Hydracker côte à côte */
  .searches-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  @media (max-width: 1100px) { .searches-row { grid-template-columns: 1fr; } }

  /* Layout 2-colonnes pour les options de post (gain ~50% de hauteur) */
  .post-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px 24px; }
  .post-grid > .post-field { margin: 0; }

  /* Barres DDL côte à côte — hauteur égale, largeur égale */
  .ddl-bars { display: grid; grid-template-columns: 1fr 1fr; grid-auto-rows: 1fr; gap: 10px; margin-top: 6px; }

  /* ---------- Suivi du post : barres à gauche, panneau à droite ----------
     Les barres occupaient une grille 2 colonnes avec une seule carte, d'où
     une moitié droite vide. On les empile et on remplit la place. */
  .post-live { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; align-items: start; margin-top: 6px; }
  .post-live-bars { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  .post-live-bars .ddl-bars { grid-template-columns: 1fr; margin-top: 0; }

  .post-panel {
    background: rgba(0,0,0,0.25);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
    min-width: 0;
  }
  .post-panel-title {
    font-family: var(--font-display); font-size: 9px; font-weight: 700;
    text-transform: uppercase; letter-spacing: .2em; color: var(--text3);
    margin-bottom: 9px;
  }

  .pp-steps { list-style: none; margin: 0; padding: 0; }
  .pp-step {
    display: flex; align-items: center; gap: 8px;
    font-size: 11.5px; line-height: 1.55; color: var(--text3);
  }
  .pp-dot {
    width: 7px; height: 7px; border-radius: 50%; flex: none;
    border: 1px solid var(--text3); background: transparent;
  }
  .pp-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pp-pct { margin-left: auto; font-size: 10px; color: var(--ember-hi); flex: none; }

  .pp-ok   { color: var(--text2); }
  .pp-ok   .pp-dot { background: var(--success); border-color: var(--success); }
  .pp-now  { color: var(--text1); font-weight: 600; }
  .pp-now  .pp-dot { background: var(--ember); border-color: var(--ember);
                     animation: pp-pulse 1.3s ease-in-out infinite; }
  .pp-err  { color: var(--red-hot); }
  .pp-err  .pp-dot { background: var(--danger); border-color: var(--danger); }
  /* opacity seule : pas de filtre, conformément à la règle de perf. */
  @keyframes pp-pulse { 0%, 100% { opacity: 1 } 50% { opacity: .35 } }

  .pp-sep { height: 1px; background: var(--border); margin: 10px 0; }

  .pp-figures { display: grid; grid-template-columns: 1fr 1fr; gap: 5px 14px; }
  .pp-figures > div { display: flex; align-items: baseline; gap: 7px; min-width: 0; }
  .pp-k { font-size: 9px; text-transform: uppercase; letter-spacing: .14em; color: var(--text3); flex: none; }
  .pp-figures b { font-size: 12px; color: var(--text1); font-weight: 600; margin-left: auto; }

  .pp-courbe { width: 100%; height: 30px; margin-top: 10px; display: block; }
  .pp-courbe polyline { fill: none; stroke-width: 1.4; vector-effect: non-scaling-stroke; }
  .pp-l-ddl { stroke: var(--success); }
  .pp-l-nzb { stroke: var(--ember); }
  .pp-legende { display: flex; align-items: center; gap: 10px; margin-top: 4px; font-size: 9px; }
  .pp-lg { display: inline-flex; align-items: center; gap: 4px; }
  .pp-lg::before { content: ""; width: 7px; height: 2px; border-radius: 1px; }
  .pp-lg-ddl { color: var(--success); }
  .pp-lg-ddl::before { background: var(--success); }
  .pp-lg-nzb { color: var(--ember-hi); }
  .pp-lg-nzb::before { background: var(--ember); }
  .pp-lg-max { margin-left: auto; color: var(--text3); }

  .pp-next { display: flex; align-items: baseline; gap: 8px; font-size: 11px; min-width: 0; }
  .pp-next b { color: var(--text1); font-weight: 600; overflow: hidden;
               text-overflow: ellipsis; white-space: nowrap; }
  .pp-reste { margin-left: auto; color: var(--ember-hi); font-size: 10px; flex: none; }

  @media (max-width: 900px) {
    .post-live { grid-template-columns: 1fr; }
  }
  @media (max-width: 900px) {
    .post-grid, .ddl-bars { grid-template-columns: 1fr; grid-auto-rows: auto; }
  }
  .ddl-bar-card {
    background: rgba(0,0,0,0.25);
    border: 1px solid var(--border);
    border-radius: 10px; padding: 10px 12px;
    display: flex; flex-direction: column; gap: 6px;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.03);
    min-width: 0; /* évite l'overflow horizontal */
  }
  .ddl-bar-card.done {
    border-color: rgba(34, 197, 94, 0.3);
    background: rgba(34, 197, 94, 0.05);
  }
  .ddl-bar-card.error {
    border-color: rgba(239, 68, 68, 0.3);
    background: rgba(239, 68, 68, 0.05);
  }
  .ddl-bar-header { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
  .ddl-bar-host {
    font-size: 11px; font-weight: 700; color: var(--blue-hot);
    text-transform: uppercase; letter-spacing: 0.4px; flex: none;
  }
  /* Débit, ETA et pourcentage : les chiffres qu'on fixe pendant un upload.
     Chasse fixe tabulaire pour qu'ils ne dansent pas en défilant. */
  .ddl-bar-speed {
    font-family: var(--font-mono); font-variant-numeric: tabular-nums;
    font-size: 13px; font-weight: 700; color: var(--ember-hi);
    letter-spacing: -.01em; margin-left: auto;
  }
  .ddl-bar-status { font-size: 11px; font-weight: 600; margin-left: auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ddl-bar-status.ok { color: #7ef0c0; }
  .ddl-bar-status.err { color: #ff9585; }
  .ddl-bar-status.skipped { color: #ffe066; }
  .ddl-bar-status.posting { color: var(--yellow); }
  .progress-fill.posting {
    background: linear-gradient(180deg, var(--yellow) 0%, var(--orange) 100%);
    animation: pulse 1s ease-in-out infinite;
  }
  @keyframes pulse { 0%,100% { opacity:1 } 50% { opacity:0.65 } }
  .ddl-bar-filename {
    font-size: 10px; color: var(--text3);
    word-break: break-all; line-height: 1.3;
  }
  .ddl-bar-row { display: flex; align-items: center; gap: 9px; }
  .ddl-bar-pct {
    font-size: 12px; color: var(--text2);
    width: 40px; text-align: right; flex: none;
    font-variant-numeric: tabular-nums;
  }
  .ddl-bar-errmsg { font-size: 11px; color: #ff9585; }
  .ddl-step { display: flex; flex-direction: column; gap: 4px; }
  .ddl-step-label {
    display: flex; justify-content: space-between; align-items: center;
    font-size: 11px; color: var(--text3);
  }
  .progress-fill.hydone {
    background: linear-gradient(180deg, var(--success) 0%, #2bb972 100%);
    box-shadow: 0 0 10px rgba(61, 220, 132, 0.45);
  }
  /* ── Bandeau de diodes ──────────────────────────────────────────
     La barre est découpée en segments par un MASQUE CSS, sans aucun
     changement de balisage : les 6 barres de l'app en héritent.
     Le motif est exprimé en unités absolues et ancré à gauche, donc
     il reste fixe pendant que `width` grandit — les diodes
     s'allument au lieu de glisser. Un segment partiel en tête donne
     l'impression du voyant en cours. */
  .progress-bar {
    --seg: 9px;
    --gap: 3px;
    height: 9px;
    background: transparent;
    border-radius: 2px;
    overflow: hidden;
    position: relative;
  }
  /* diodes éteintes, sur toute la largeur */
  .progress-bar::before {
    content: "";
    position: absolute; inset: 0;
    background: rgba(255, 255, 255, 0.055);
    -webkit-mask-image: repeating-linear-gradient(90deg, #000 0 var(--seg), transparent var(--seg) calc(var(--seg) + var(--gap)));
            mask-image: repeating-linear-gradient(90deg, #000 0 var(--seg), transparent var(--seg) calc(var(--seg) + var(--gap)));
  }
  .progress-fill {
    position: relative;
    height: 100%;
    background: var(--grad-primary);
    border-radius: 2px;
    transition: width 0.12s linear;
    box-shadow: 0 0 8px var(--accent-glow);
    -webkit-mask-image: repeating-linear-gradient(90deg, #000 0 var(--seg), transparent var(--seg) calc(var(--seg) + var(--gap)));
            mask-image: repeating-linear-gradient(90deg, #000 0 var(--seg), transparent var(--seg) calc(var(--seg) + var(--gap)));
  }

  /* ── Bilan de session ─────────────────────────────────────────────── */
  .bilan-box {
    position: relative;
    background: linear-gradient(180deg, rgba(61,220,132,.06), rgba(61,220,132,.02));
    border: 1px solid rgba(61,220,132,.26);
    border-radius: 11px; padding: 12px 14px; margin-bottom: 10px;
  }
  .bilan-box.bilan-ko {
    background: linear-gradient(180deg, rgba(255,200,61,.07), rgba(255,200,61,.02));
    border-color: rgba(255,200,61,.3);
  }
  .bilan-head {
    display: flex; align-items: center; gap: 10px; margin-bottom: 9px;
    font-family: var(--font-display); font-size: 10px; font-weight: 700;
    letter-spacing: .18em; text-transform: uppercase; color: var(--success);
  }
  .bilan-ko .bilan-head { color: var(--warning); }
  .bilan-count { font-family: var(--font-mono); font-size: 10px; letter-spacing: .04em;
    color: var(--text3); text-transform: none; font-weight: 500; }
  .bilan-x { margin-left: auto; background: transparent; border: 0; color: var(--text3);
    font-size: 16px; line-height: 1; cursor: pointer; padding: 0 2px; }
  .bilan-x:hover { color: var(--text); }
  .bilan-list { display: flex; flex-direction: column; gap: 3px; max-height: 190px; overflow-y: auto; }
  .bilan-row { display: flex; align-items: center; gap: 7px; }
  .bilan-name {
    font-family: var(--font-mono); font-size: 10.5px; color: var(--text2);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0;
  }
  .bilan-tag {
    font-family: var(--font-display); font-size: 8.5px; font-weight: 700; letter-spacing: .1em;
    padding: 2px 6px; border-radius: 4px; flex: none; border: 1px solid transparent;
  }
  .bilan-on  { color: var(--success); border-color: rgba(61,220,132,.35); background: rgba(61,220,132,.1); }
  .bilan-off { color: var(--warning); border-color: rgba(255,200,61,.4);  background: rgba(255,200,61,.12); }
  .bilan-fix {
    width: 100%; margin-top: 10px; padding: 9px;
    background: linear-gradient(180deg, var(--ember-hi), var(--ember-deep));
    color: #1a0d04; border: 0; border-radius: 7px; cursor: pointer;
    font-family: var(--font-display); font-size: 11px; font-weight: 700; letter-spacing: .08em;
  }
  .bilan-fix:hover { filter: brightness(1.07); }

  /* Récapitulatif (repliable) */
  .recap-box {
    background: linear-gradient(180deg, rgba(255, 107, 26, 0.05) 0%, rgba(255, 107, 26, 0.02) 100%);
    border: 1px solid rgba(255, 107, 26, 0.2);
    border-radius: 14px; overflow: hidden;
    box-shadow: inset 0 1px 0 rgba(255,255,255,0.05);
  }
  .recap-title {
    display: flex; align-items: center; gap: 6px; width: 100%;
    background: transparent; border: 0; padding: 10px 18px;
    font-family: var(--font-display); font-size: 10px; font-weight: 700; color: var(--ember-hi);
    text-transform: uppercase; letter-spacing: .2em;
    cursor: pointer; text-align: left;
  }
  .recap-title:hover { background: rgba(255,255,255,0.02); }
  .recap-box.open .recap-title { border-bottom: 1px solid rgba(255, 107, 26, 0.2); }
  .recap-body { padding: 12px 18px 14px; }
  .recap-row {
    display: flex; justify-content: space-between; align-items: baseline;
    padding: 5px 0; border-bottom: 1px solid rgba(255,255,255,0.06);
    font-size: 12px;
  }
  .recap-row:last-child { border-bottom: none; }
  .recap-key {
    color: var(--text3); flex: none; width: 120px;
    text-transform: uppercase; font-size: 10px; letter-spacing: 0.5px;
  }
  .recap-val { color: var(--text); text-align: right; flex: 1; }
  .recap-id { color: var(--red-hot); }
  .recap-type {
    background: rgba(255, 107, 26, 0.15); color: var(--blue-hot);
    font-size: 10px; padding: 1px 7px; border-radius: 9999px; margin-left: 4px;
  }
  .recap-file { color: var(--text2); font-size: 11px; word-break: break-all; }

  .nfo-preview {
    background: rgba(0,0,0,0.3);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
  }
  .nfo-preview-header {
    display: flex; justify-content: space-between; align-items: center;
    gap: 8px; width: 100%;
    padding: 10px 14px;
    background: transparent; border: 0;
    font-size: 11px; font-weight: 600; color: var(--text2);
    text-transform: uppercase; letter-spacing: 1.2px;
    cursor: pointer; text-align: left;
  }
  .nfo-preview-header:hover { background: rgba(255,255,255,0.03); }
  .nfo-preview.open .nfo-preview-header { border-bottom: 1px solid var(--border); }
  .nfo-preview .nfo-preview-body { margin: 0 14px 14px; }
  .nfo-edit {
    margin: 0 14px 14px;
    width: calc(100% - 28px);
    background: rgba(0,0,0,0.3);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px;
    font-family: ui-monospace, monospace;
    font-size: 11px;
    resize: vertical;
    min-height: 200px;
  }
  .nfo-edit:focus { outline: none; border-color: var(--blue-hot); }
  .nfo-preview-body {
    margin: 0; padding: 12px;
    background: rgba(0,0,0,0.4);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px; line-height: 1.45;
    white-space: pre; overflow-x: auto;
    max-height: 360px; overflow-y: auto;
  }
</style>
