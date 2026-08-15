function gobby() {
  return {
    tab: 'home', // home | series | movie | audio | book | files | watch
    q: '',
    searchOpen: false,      // header search expanded from its magnifier
    searchResults: [],      // global search hits (all kinds)
    groups: [],
    loose: [],
    watch: [],
    homeCont: [], // "continue watching" shelf
    homeNew: [],  // "recently added" shelf
    newWatch: '',
    theme: 'dark',
    lang: 'es',

    // navigation: a stack of views. top of stack is what's shown.
    // { view: 'home' } | { view: 'series', group } | { view: 'item', item }
    stack: [{ view: 'home' }],

    // library info (root path + item count)
    info: { root: '', total: 0 },

    showFilters: false,
    filters: { ext: '', year: '', cover: '', rating_min: '', genre: '' },
    filterQS() {
      const p = new URLSearchParams();
      const f = this.filters;
      if (f.ext) p.set('ext', f.ext);
      if (f.year) { p.set('year_min', f.year); p.set('year_max', f.year); }
      if (f.cover) p.set('cover', f.cover);
      if (f.rating_min) p.set('rating_min', f.rating_min);
      if (f.genre) p.set('genre', f.genre);
      return p.toString();
    },
    activeChips() {
      const c = [];
      const f = this.filters;
      if (f.ext) c.push({ k: 'ext', label: '.' + f.ext });
      if (f.year) c.push({ k: 'year', label: f.year });
      if (f.cover) c.push({ k: 'cover', label: f.cover === 'with' ? '★ con carátula' : 'sin carátula' });
      if (f.rating_min) c.push({ k: 'rating_min', label: '≥ ' + f.rating_min + '★' });
      if (f.genre) c.push({ k: 'genre', label: f.genre });
      return c;
    },
    removeChip(k) {
      this.filters[k === 'rating' ? 'rating_min' : k] = '';
      this.load();
    },
    clearFilters() {
      this.filters = { ext: '', year: '', cover: '', rating_min: '', genre: '' };
      this.load();
    },
    // Home shelves come from /api/home (no server filters), so apply the active
    // filters client-side there — otherwise the funnel does nothing on Home.
    applyFilters(arr) {
      const f = this.filters;
      return (arr || []).filter(it => {
        if (f.ext && !(it.rel_path || '').toLowerCase().endsWith('.' + f.ext)) return false;
        if (f.year && String(it.year) !== String(f.year)) return false;
        if (f.cover === 'with' && !it.has_cover) return false;
        if (f.cover === 'without' && it.has_cover) return false;
        if (f.rating_min && (it.rating || 0) < +f.rating_min) return false;
        if (f.genre && !((it.genre || '').toLowerCase().includes(f.genre.toLowerCase()))) return false;
        return true;
      });
    },

    menuOpen: false, // header overflow menu (cover fetching)

    // background progress (scan + enrich), unified into one bar
    busy: false,
    prog: { done: 0, total: 0, found: 0, phase: '' },
    pollTimer: null,
    sections: {}, // which tabs have content (from /api/info)

    strings: {
      es: {
        search: 'Buscar en tu biblioteca…', series: 'Series', movie: 'Películas',
        audio: 'Música', book: 'Libros', files: 'Ficheros', watch: 'Pendientes',
        albums: 'Álbumes', authors: 'Autores', movies: 'Películas', singles: 'Sueltos', folders: 'Carpetas', looseFiles: 'Sueltos', filesUnit: 'ficheros', filterFiles: 'Filtrar ficheros…',
        empty: 'Nada por aquí todavía.', episodes: 'episodios', tracks: 'temas', books: 'libros',
        title: 'Título', notes: 'Notas', rating: 'Valoración', location: 'Ubicación', reveal: 'Abrir carpeta', uploadHere: 'Subir aquí', deleteFile: 'Borrar del disco', deleteConfirm: '¿Borrar «{name}» del disco? No se puede deshacer.', deleteFailed: 'No se pudo borrar.',
        director: 'Dirección', cast: 'Reparto', back: 'Atrás', add: 'Añadir',
        enrichThemes: 'Buscar carátulas', theme: 'Cambiar tema', fetching: 'Buscando carátulas', sound: 'Sonido', language: 'Idioma',
        forceAll: 'Rebuscar todo (sobrescribe)', stop: 'Parar',
        identify: 'ID (IMDb, ej. tt1375666)', update: 'Actualizar', updating: 'Actualizando…', editId: 'Editar ID', openRef: 'Ver referencia',
        section: 'Sección', autoSection: 'Automática', movieSection: 'Películas', seriesSection: 'Series', musicSection: 'Música', bookSection: 'Libros', filesSection: 'Ficheros',
        download: 'Descargar',
        filters: 'Filtros', fFormat: 'Formato', fYear: 'Año',
        fCover: 'Carátula', fCoverAll: 'Todas', fCoverWith: 'Con carátula', fCoverWithout: 'Sin carátula',
        fRating: 'Valoración mín.', fAny: 'Cualquiera', fGenre: 'Género', clear: 'Limpiar',
        openExternal: 'Copiar enlace', openWith: 'Abrir con…',
        copied: 'Copiado', openStream: 'Abrir', playHere: 'Reproducir aquí', subsOff: 'Sin subtítulos',
        subStyle: 'Subtítulos', subSize: 'Tamaño', subColor: 'Color', subBox: 'Fondo', on: 'Sí', off: 'No', playerSettings: 'Audio y subtítulos',
        scanning: 'Escaneando ficheros', source: 'Fuente', local: 'archivo local',
        resolution: 'Resolución', vcodec: 'Vídeo', acodec: 'Audio', channels: 'Canales', languages: 'Idiomas',
        audioWarn: 'Audio {c}: el navegador no lo reproduce. Verás vídeo sin sonido — ábrelo en un reproductor externo (VLC / MX Player).',
        more: 'Más',
        watchSearchPlaceholder: 'Buscar por título…',
        watchEmpty: 'Busca algo que quieras ver, escuchar o leer.', remove: 'Quitar', other: 'Otro', home: 'Inicio', watchedDone: 'Visto', markDone: 'Marcar como visto', markUndone: 'Marcar como pendiente',
        game: 'Juego', continueW: 'Seguir viendo', newlyAdded: 'Novedades', preview: 'Previsualizar', send: 'Enviar', addManual: 'Añadir manualmente',
        manualOnly: 'Sin búsqueda online — se añade manualmente', addAsType: 'Añadir como {type}',
        season: 'Temporada', otherEps: 'Otros', mArtist: 'Artista', mAuthor: 'Autor', mSeries: 'Serie', mAlbum: 'Álbum', mCollection: 'Colección',
        changeFolder: 'Cambiar carpeta', exportDb: 'Exportar base de datos', importDb: 'Importar base de datos',
        folderPrompt: 'Ruta de la carpeta a escanear:', importConfirm: 'Reinicia Gobby para cargar la base importada.',
        libraries: 'Bibliotecas', view: 'Ver', viewing: 'Viendo', rebind: 'Usar esta carpeta', rebindHint: 'Re-vincular esta biblioteca a la carpeta donde está Gobby ahora', rebindConfirm: '¿Re-vincular esta biblioteca a la carpeta actual de Gobby?', unreachable: 'no accesible', unavailable: 'No disponible', unavailableNote: 'Este elemento no es accesible ahora mismo (fichero movido o unidad desconectada).', importMerged: 'Importadas {n} biblioteca(s).', importNone: 'Nada nuevo que importar.',
        offlineTitle: 'Gobby se ha ido a casa', offlineSub: 'El pequeño elfo cerró el servidor. Vuelve a abrir Gobby para seguir. 🧦',
        clearFilters: 'Limpiar filtros',
        connect: 'Conectar dispositivos',
        devices: 'Dispositivos conectados', thisDevice: 'este dispositivo', noDevices: 'Ningún dispositivo aún.', now: 'ahora',
        changeCover: 'Cambiar carátula', coverFromUrl: 'Desde URL', coverUrlPrompt: 'Pega la URL de la imagen:', openTab: 'Abrir en pestaña', castTo: 'Transmitir a TV', moreActions: 'Más', castStop: 'Parar transmisión', castFailed: 'No se pudo transmitir. El Chromecast necesita HTTP y formato compatible (mp4).', continueAt: 'Continúa:', resumeHere: 'Reanudar',
        customFields: 'Campos', addField: 'Añadir campo', fieldName: 'Nombre', fieldValue: 'Valor',
        tunnelStart: 'Abrir túnel público', tunnelStop: 'Cerrar túnel', tunnelStarting: 'Preparando túnel…', tabLocal: 'Red local', tabInternet: 'Internet',
        mcpConnect: 'Conectar MCP', mcpIntro: 'Gobby expone un servidor MCP para que Claude pueda buscar en tu biblioteca y gestionar la lista de pendientes. Añádelo con una de estas formas:',
        copy: 'Copiar', mcpUrl: 'URL del servidor', mcpCli: 'Con la CLI de Claude Code', mcpTools: 'Herramientas: search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'Acerca de', aboutDeps: 'Dependencias', aboutApis: 'APIs y servicios',
        updCheck: 'Buscar actualización', updChecking: 'Comprobando…', updLatest: 'Estás en la última versión', updNew: 'Nueva versión: {v}', updApply: 'Actualizar Gobby', updApplying: 'Actualizando…', updDone: 'Actualizado — reinicia Gobby para usar la nueva versión.', updFail: 'No se pudo actualizar',
        dServer: 'servidor', dUI: 'interfaz', dSound: 'sonido', dMcp: 'servidor MCP', dDb: 'base de datos', dTags: 'metadatos de audio', dQr: 'códigos QR', dFfmpeg: 'vídeo, audio y subtítulos', dEpub: 'lector de epub', dZip: 'lectura de zip',
        aMovies: 'pelis y series', aBooks: 'libros', aMusic: 'música', aMusicCovers: 'carátulas de música', aGames: 'juegos', aFallback: 'carátulas alt.', aRemote: 'acceso remoto', aCast: 'reproducir en TV',
      },
      en: {
        search: 'Search your library…', series: 'Series', movie: 'Movies',
        audio: 'Music', book: 'Books', files: 'Files', watch: 'Watchlist',
        albums: 'Albums', authors: 'Authors', movies: 'Movies', singles: 'Singles', folders: 'Folders', looseFiles: 'Loose', filesUnit: 'files', filterFiles: 'Filter files…',
        empty: 'Nothing here yet.', episodes: 'episodes', tracks: 'tracks', books: 'books',
        title: 'Title', notes: 'Notes', rating: 'Rating', location: 'Location', reveal: 'Open folder', uploadHere: 'Upload here', deleteFile: 'Delete from disk', deleteConfirm: 'Delete "{name}" from disk? This cannot be undone.', deleteFailed: 'Could not delete.',
        director: 'Director', cast: 'Cast', back: 'Back', add: 'Add',
        enrichThemes: 'Fetch covers', theme: 'Toggle theme', fetching: 'Fetching covers', sound: 'Sound', language: 'Language',
        forceAll: 'Re-fetch all (overwrite)', stop: 'Stop',
        identify: 'ID (IMDb, e.g. tt1375666)', update: 'Update', updating: 'Updating…', editId: 'Edit ID', openRef: 'Open reference',
        section: 'Section', autoSection: 'Automatic', movieSection: 'Movies', seriesSection: 'Series', musicSection: 'Music', bookSection: 'Books', filesSection: 'Files',
        download: 'Download',
        filters: 'Filters', fFormat: 'Format', fYear: 'Year',
        fCover: 'Cover', fCoverAll: 'All', fCoverWith: 'With cover', fCoverWithout: 'Without cover',
        fRating: 'Min. rating', fAny: 'Any', fGenre: 'Genre', clear: 'Clear',
        openExternal: 'Copy link', openWith: 'Open with…',
        copied: 'Copied', openStream: 'Open', playHere: 'Play here', subsOff: 'No subtitles',
        subStyle: 'Subtitles', subSize: 'Size', subColor: 'Colour', subBox: 'Box', on: 'On', off: 'Off', playerSettings: 'Audio & subtitles',
        scanning: 'Scanning files', source: 'Source', local: 'local file',
        resolution: 'Resolution', vcodec: 'Video', acodec: 'Audio', channels: 'Channels', languages: 'Languages',
        audioWarn: "Audio {c}: your browser can't decode it. Video will play with no sound — open it in an external player (VLC / MX Player).",
        more: 'More',
        watchSearchPlaceholder: 'Search by title…',
        watchEmpty: 'Search for something to watch, listen to or read.', remove: 'Remove', other: 'Other', home: 'Home', watchedDone: 'Done', markDone: 'Mark as done', markUndone: 'Mark as pending',
        game: 'Game', continueW: 'Continue watching', newlyAdded: 'Recently added', preview: 'Preview', send: 'Send', addManual: 'Add manually',
        manualOnly: 'No online search — added manually', addAsType: 'Add as {type}',
        season: 'Season', otherEps: 'Other', mArtist: 'Artist', mAuthor: 'Author', mSeries: 'Series', mAlbum: 'Album', mCollection: 'Collection',
        changeFolder: 'Change folder', exportDb: 'Export database', importDb: 'Import database',
        folderPrompt: 'Path of the folder to scan:', importConfirm: 'Restart Gobby to load the imported database.',
        libraries: 'Libraries', view: 'View', viewing: 'Viewing', rebind: 'Use this folder', rebindHint: 'Re-point this library to the folder Gobby is in now', rebindConfirm: 'Re-point this library to Gobby’s current folder?', unreachable: 'unreachable', unavailable: 'Unavailable', unavailableNote: 'This item is not reachable right now (file moved or drive disconnected).', importMerged: 'Imported {n} library(ies).', importNone: 'Nothing new to import.',
        offlineTitle: 'Gobby has gone home', offlineSub: 'The little elf shut the server down. Open Gobby again to carry on. 🧦',
        clearFilters: 'Clear filters',
        connect: 'Connect devices',
        devices: 'Connected devices', thisDevice: 'this device', noDevices: 'No devices yet.', now: 'now',
        changeCover: 'Change cover', coverFromUrl: 'From URL', coverUrlPrompt: 'Paste the image URL:', openTab: 'Open in tab', castTo: 'Cast to TV', moreActions: 'More', castStop: 'Stop casting', castFailed: 'Could not cast. Chromecast needs HTTP and a compatible format (mp4).', continueAt: 'Up to:', resumeHere: 'Resume',
        customFields: 'Fields', addField: 'Add field', fieldName: 'Name', fieldValue: 'Value',
        tunnelStart: 'Open public tunnel', tunnelStop: 'Close tunnel', tunnelStarting: 'Preparing tunnel…', tabLocal: 'Local network', tabInternet: 'Internet',
        mcpConnect: 'Connect MCP', mcpIntro: 'Gobby exposes an MCP server so Claude can search your library and manage the watchlist. Add it one of these ways:',
        copy: 'Copy', mcpUrl: 'Server URL', mcpCli: 'With the Claude Code CLI', mcpTools: 'Tools: search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'About', aboutDeps: 'Dependencies', aboutApis: 'APIs & services',
        updCheck: 'Check for update', updChecking: 'Checking…', updLatest: "You're on the latest version", updNew: 'New version: {v}', updApply: 'Update Gobby', updApplying: 'Updating…', updDone: 'Updated — restart Gobby to use the new version.', updFail: 'Update failed',
        dServer: 'server', dUI: 'UI', dSound: 'sound', dMcp: 'MCP server', dDb: 'database', dTags: 'audio metadata', dQr: 'QR codes', dFfmpeg: 'video, audio & subtitles', dEpub: 'epub reader', dZip: 'zip reading',
        aMovies: 'movies & series', aBooks: 'books', aMusic: 'music', aMusicCovers: 'music covers', aGames: 'games', aFallback: 'fallback covers', aRemote: 'remote access', aCast: 'cast to TV',
      },
      fr: {
        search: 'Rechercher dans votre bibliothèque…', series: 'Séries', movie: 'Films',
        audio: 'Musique', book: 'Livres', files: 'Fichiers', watch: 'À voir',
        albums: 'Albums', authors: 'Auteurs', movies: 'Films', singles: 'Isolés', folders: 'Dossiers', looseFiles: 'Isolés', filesUnit: 'fichiers', filterFiles: 'Filtrer les fichiers…',
        empty: 'Rien ici pour le moment.', episodes: 'épisodes', tracks: 'pistes', books: 'livres',
        title: 'Titre', notes: 'Notes', rating: 'Note', location: 'Emplacement', reveal: 'Ouvrir le dossier', uploadHere: 'Téléverser ici', deleteFile: 'Supprimer du disque', deleteConfirm: 'Supprimer « {name} » du disque ? Irréversible.', deleteFailed: 'Suppression impossible.',
        director: 'Réalisation', cast: 'Distribution', back: 'Retour', add: 'Ajouter',
        enrichThemes: 'Chercher les jaquettes', theme: 'Changer de thème', fetching: 'Recherche des jaquettes', sound: 'Son', language: 'Langue',
        forceAll: 'Tout rechercher (écrase)', stop: 'Arrêter',
        identify: 'ID (IMDb, ex. tt1375666)', update: 'Mettre à jour', updating: 'Mise à jour…', editId: "Modifier l'ID", openRef: 'Voir la référence',
        section: 'Section', autoSection: 'Automatique', movieSection: 'Films', seriesSection: 'Séries', musicSection: 'Musique', bookSection: 'Livres', filesSection: 'Fichiers',
        download: 'Télécharger',
        filters: 'Filtres', fFormat: 'Format', fYear: 'Année',
        fCover: 'Jaquette', fCoverAll: 'Toutes', fCoverWith: 'Avec jaquette', fCoverWithout: 'Sans jaquette',
        fRating: 'Note min.', fAny: 'Toutes', fGenre: 'Genre', clear: 'Effacer',
        openExternal: 'Copier le lien', openWith: 'Ouvrir avec…',
        copied: 'Copié', openStream: 'Ouvrir', playHere: 'Lire ici', subsOff: 'Sans sous-titres',
        subStyle: 'Sous-titres', subSize: 'Taille', subColor: 'Couleur', subBox: 'Fond', on: 'Oui', off: 'Non', playerSettings: 'Audio et sous-titres',
        scanning: 'Analyse des fichiers', source: 'Source', local: 'fichier local',
        resolution: 'Résolution', vcodec: 'Vidéo', acodec: 'Audio', channels: 'Canaux', languages: 'Langues',
        audioWarn: "Audio {c} : votre navigateur ne peut pas le décoder. La vidéo sera muette — ouvrez-la dans un lecteur externe (VLC / MX Player).",
        more: 'Plus',
        watchSearchPlaceholder: 'Rechercher par titre…',
        watchEmpty: 'Cherchez quelque chose à voir, écouter ou lire.', remove: 'Retirer', other: 'Autre', home: 'Accueil', watchedDone: 'Vu', markDone: 'Marquer comme vu', markUndone: 'Marquer comme à voir',
        game: 'Jeu', continueW: 'Reprendre', newlyAdded: 'Nouveautés', preview: 'Aperçu', send: 'Envoyer', addManual: 'Ajouter manuellement',
        manualOnly: 'Sans recherche en ligne — ajout manuel', addAsType: 'Ajouter comme {type}',
        season: 'Saison', otherEps: 'Autres', mArtist: 'Artiste', mAuthor: 'Auteur', mSeries: 'Série', mAlbum: 'Album', mCollection: 'Collection',
        changeFolder: 'Changer de dossier', exportDb: 'Exporter la base', importDb: 'Importer une base',
        folderPrompt: 'Chemin du dossier à analyser :', importConfirm: 'Redémarrez Gobby pour charger la base importée.',
        libraries: 'Bibliothèques', view: 'Voir', viewing: 'Affichée', rebind: 'Utiliser ce dossier', rebindHint: 'Ré-associer cette bibliothèque au dossier où se trouve Gobby maintenant', rebindConfirm: 'Ré-associer cette bibliothèque au dossier actuel de Gobby ?', unreachable: 'inaccessible', unavailable: 'Indisponible', unavailableNote: "Cet élément n'est pas accessible pour le moment (fichier déplacé ou disque déconnecté).", importMerged: '{n} bibliothèque(s) importée(s).', importNone: 'Rien de nouveau à importer.',
        offlineTitle: 'Gobby est rentré chez lui', offlineSub: 'Le petit elfe a éteint le serveur. Rouvre Gobby pour continuer. 🧦',
        clearFilters: 'Effacer les filtres',
        connect: 'Connecter des appareils',
        devices: 'Appareils connectés', thisDevice: 'cet appareil', noDevices: 'Aucun appareil pour le moment.', now: 'maintenant',
        changeCover: 'Changer la jaquette', coverFromUrl: 'Depuis une URL', coverUrlPrompt: "Collez l'URL de l'image :", openTab: 'Ouvrir dans un onglet', castTo: 'Diffuser sur la TV', moreActions: 'Plus', castStop: 'Arrêter', castFailed: 'Diffusion impossible. Chromecast requiert HTTP et un format compatible (mp4).', continueAt: 'Repris à :', resumeHere: 'Reprendre',
        customFields: 'Champs', addField: 'Ajouter un champ', fieldName: 'Nom', fieldValue: 'Valeur',
        tunnelStart: 'Ouvrir un tunnel public', tunnelStop: 'Fermer le tunnel', tunnelStarting: 'Préparation du tunnel…', tabLocal: 'Réseau local', tabInternet: 'Internet',
        mcpConnect: 'Connecter MCP', mcpIntro: 'Gobby expose un serveur MCP pour que Claude puisse chercher dans votre bibliothèque et gérer la liste à voir. Ajoutez-le de l’une de ces façons :',
        copy: 'Copier', mcpUrl: 'URL du serveur', mcpCli: 'Avec la CLI de Claude Code', mcpTools: 'Outils : search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'À propos', aboutDeps: 'Dépendances', aboutApis: 'API et services',
        updCheck: 'Chercher une mise à jour', updChecking: 'Vérification…', updLatest: 'Vous avez la dernière version', updNew: 'Nouvelle version : {v}', updApply: 'Mettre à jour Gobby', updApplying: 'Mise à jour…', updDone: 'Mis à jour — redémarrez Gobby pour utiliser la nouvelle version.', updFail: 'Échec de la mise à jour',
        dServer: 'serveur', dUI: 'interface', dSound: 'son', dMcp: 'serveur MCP', dDb: 'base de données', dTags: 'métadonnées audio', dQr: 'codes QR', dFfmpeg: 'vidéo, audio et sous-titres', dEpub: 'lecteur epub', dZip: 'lecture zip',
        aMovies: 'films et séries', aBooks: 'livres', aMusic: 'musique', aMusicCovers: 'jaquettes musique', aGames: 'jeux', aFallback: 'jaquettes de secours', aRemote: 'accès distant', aCast: 'diffuser sur TV',
      },
      de: {
        search: 'Deine Bibliothek durchsuchen…', series: 'Serien', movie: 'Filme',
        audio: 'Musik', book: 'Bücher', files: 'Dateien', watch: 'Merkliste',
        albums: 'Alben', authors: 'Autoren', movies: 'Filme', singles: 'Einzeln', folders: 'Ordner', looseFiles: 'Einzeln', filesUnit: 'Dateien', filterFiles: 'Dateien filtern…',
        empty: 'Hier ist noch nichts.', episodes: 'Folgen', tracks: 'Titel', books: 'Bücher',
        title: 'Titel', notes: 'Notizen', rating: 'Bewertung', location: 'Speicherort', reveal: 'Ordner öffnen', uploadHere: 'Hier hochladen', deleteFile: 'Von Festplatte löschen', deleteConfirm: '„{name}“ von der Festplatte löschen? Nicht umkehrbar.', deleteFailed: 'Löschen fehlgeschlagen.',
        director: 'Regie', cast: 'Besetzung', back: 'Zurück', add: 'Hinzufügen',
        enrichThemes: 'Cover suchen', theme: 'Thema wechseln', fetching: 'Cover werden gesucht', sound: 'Ton', language: 'Sprache',
        forceAll: 'Alles neu suchen (überschreibt)', stop: 'Stopp',
        identify: 'ID (IMDb, z. B. tt1375666)', update: 'Aktualisieren', updating: 'Wird aktualisiert…', editId: 'ID bearbeiten', openRef: 'Referenz öffnen',
        section: 'Bereich', autoSection: 'Automatisch', movieSection: 'Filme', seriesSection: 'Serien', musicSection: 'Musik', bookSection: 'Bücher', filesSection: 'Dateien',
        download: 'Herunterladen',
        filters: 'Filter', fFormat: 'Format', fYear: 'Jahr',
        fCover: 'Cover', fCoverAll: 'Alle', fCoverWith: 'Mit Cover', fCoverWithout: 'Ohne Cover',
        fRating: 'Mind. Bewertung', fAny: 'Beliebig', fGenre: 'Genre', clear: 'Löschen',
        openExternal: 'Link kopieren', openWith: 'Öffnen mit…',
        copied: 'Kopiert', openStream: 'Öffnen', playHere: 'Hier abspielen', subsOff: 'Keine Untertitel',
        subStyle: 'Untertitel', subSize: 'Größe', subColor: 'Farbe', subBox: 'Box', on: 'An', off: 'Aus', playerSettings: 'Audio & Untertitel',
        scanning: 'Dateien werden gescannt', source: 'Quelle', local: 'lokale Datei',
        resolution: 'Auflösung', vcodec: 'Video', acodec: 'Audio', channels: 'Kanäle', languages: 'Sprachen',
        audioWarn: 'Audio {c}: Dein Browser kann es nicht decodieren. Das Video läuft ohne Ton — öffne es in einem externen Player (VLC / MX Player).',
        more: 'Mehr',
        watchSearchPlaceholder: 'Nach Titel suchen…',
        watchEmpty: 'Suche etwas zum Ansehen, Anhören oder Lesen.', remove: 'Entfernen', other: 'Andere', home: 'Start', watchedDone: 'Gesehen', markDone: 'Als gesehen markieren', markUndone: 'Als offen markieren',
        game: 'Spiel', continueW: 'Weiterschauen', newlyAdded: 'Neu hinzugefügt', preview: 'Vorschau', send: 'Senden', addManual: 'Manuell hinzufügen',
        manualOnly: 'Keine Online-Suche — manuell hinzugefügt', addAsType: 'Als {type} hinzufügen',
        season: 'Staffel', otherEps: 'Andere', mArtist: 'Künstler', mAuthor: 'Autor', mSeries: 'Serie', mAlbum: 'Album', mCollection: 'Sammlung',
        changeFolder: 'Ordner wechseln', exportDb: 'Datenbank exportieren', importDb: 'Datenbank importieren',
        folderPrompt: 'Pfad des zu scannenden Ordners:', importConfirm: 'Starte Gobby neu, um die importierte Datenbank zu laden.',
        libraries: 'Bibliotheken', view: 'Ansehen', viewing: 'Aktiv', rebind: 'Diesen Ordner verwenden', rebindHint: 'Diese Bibliothek mit dem aktuellen Gobby-Ordner verknüpfen', rebindConfirm: 'Diese Bibliothek mit Gobbys aktuellem Ordner verknüpfen?', unreachable: 'nicht erreichbar', unavailable: 'Nicht verfügbar', unavailableNote: 'Dieses Element ist gerade nicht erreichbar (Datei verschoben oder Laufwerk getrennt).', importMerged: '{n} Bibliothek(en) importiert.', importNone: 'Nichts Neues zu importieren.',
        offlineTitle: 'Gobby ist nach Hause gegangen', offlineSub: 'Der kleine Elf hat den Server beendet. Öffne Gobby erneut, um weiterzumachen. 🧦',
        clearFilters: 'Filter löschen',
        connect: 'Geräte verbinden',
        devices: 'Verbundene Geräte', thisDevice: 'dieses Gerät', noDevices: 'Noch keine Geräte.', now: 'jetzt',
        changeCover: 'Cover ändern', coverFromUrl: 'Von URL', coverUrlPrompt: 'Bild-URL einfügen:', openTab: 'In Tab öffnen', castTo: 'An TV streamen', moreActions: 'Mehr', castStop: 'Stoppen', castFailed: 'Casting fehlgeschlagen. Chromecast braucht HTTP und ein kompatibles Format (mp4).', continueAt: 'Bis:', resumeHere: 'Fortsetzen',
        customFields: 'Felder', addField: 'Feld hinzufügen', fieldName: 'Name', fieldValue: 'Wert',
        tunnelStart: 'Öffentlichen Tunnel öffnen', tunnelStop: 'Tunnel schließen', tunnelStarting: 'Tunnel wird vorbereitet…', tabLocal: 'Lokales Netz', tabInternet: 'Internet',
        mcpConnect: 'MCP verbinden', mcpIntro: 'Gobby stellt einen MCP-Server bereit, damit Claude deine Bibliothek durchsuchen und die Merkliste verwalten kann. Füge ihn auf eine dieser Weisen hinzu:',
        copy: 'Kopieren', mcpUrl: 'Server-URL', mcpCli: 'Mit der Claude-Code-CLI', mcpTools: 'Werkzeuge: search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'Über', aboutDeps: 'Abhängigkeiten', aboutApis: 'APIs & Dienste',
        updCheck: 'Nach Update suchen', updChecking: 'Wird geprüft…', updLatest: 'Du hast die neueste Version', updNew: 'Neue Version: {v}', updApply: 'Gobby aktualisieren', updApplying: 'Wird aktualisiert…', updDone: 'Aktualisiert — starte Gobby neu, um die neue Version zu nutzen.', updFail: 'Update fehlgeschlagen',
        dServer: 'Server', dUI: 'Oberfläche', dSound: 'Ton', dMcp: 'MCP-Server', dDb: 'Datenbank', dTags: 'Audio-Metadaten', dQr: 'QR-Codes', dFfmpeg: 'Video, Audio & Untertitel', dEpub: 'epub-Leser', dZip: 'zip-Lesen',
        aMovies: 'Filme & Serien', aBooks: 'Bücher', aMusic: 'Musik', aMusicCovers: 'Musik-Cover', aGames: 'Spiele', aFallback: 'Ersatz-Cover', aRemote: 'Fernzugriff', aCast: 'auf TV streamen',
      },
      it: {
        search: 'Cerca nella tua libreria…', series: 'Serie', movie: 'Film',
        audio: 'Musica', book: 'Libri', files: 'File', watch: 'Da vedere',
        albums: 'Album', authors: 'Autori', movies: 'Film', singles: 'Sciolti', folders: 'Cartelle', looseFiles: 'Sciolti', filesUnit: 'file', filterFiles: 'Filtra file…',
        empty: 'Qui non c’è ancora niente.', episodes: 'episodi', tracks: 'tracce', books: 'libri',
        title: 'Titolo', notes: 'Note', rating: 'Valutazione', location: 'Posizione', reveal: 'Apri cartella', uploadHere: 'Carica qui', deleteFile: 'Elimina dal disco', deleteConfirm: 'Eliminare «{name}» dal disco? Irreversibile.', deleteFailed: 'Impossibile eliminare.',
        director: 'Regia', cast: 'Cast', back: 'Indietro', add: 'Aggiungi',
        enrichThemes: 'Cerca copertine', theme: 'Cambia tema', fetching: 'Ricerca copertine', sound: 'Suono', language: 'Lingua',
        forceAll: 'Ricerca tutto (sovrascrive)', stop: 'Ferma',
        identify: 'ID (IMDb, es. tt1375666)', update: 'Aggiorna', updating: 'Aggiornamento…', editId: 'Modifica ID', openRef: 'Apri riferimento',
        section: 'Sezione', autoSection: 'Automatica', movieSection: 'Film', seriesSection: 'Serie', musicSection: 'Musica', bookSection: 'Libri', filesSection: 'File',
        download: 'Scarica',
        filters: 'Filtri', fFormat: 'Formato', fYear: 'Anno',
        fCover: 'Copertina', fCoverAll: 'Tutte', fCoverWith: 'Con copertina', fCoverWithout: 'Senza copertina',
        fRating: 'Valutazione min.', fAny: 'Qualsiasi', fGenre: 'Genere', clear: 'Pulisci',
        openExternal: 'Copia link', openWith: 'Apri con…',
        copied: 'Copiato', openStream: 'Apri', playHere: 'Riproduci qui', subsOff: 'Senza sottotitoli',
        subStyle: 'Sottotitoli', subSize: 'Dimensione', subColor: 'Colore', subBox: 'Sfondo', on: 'Sì', off: 'No', playerSettings: 'Audio e sottotitoli',
        scanning: 'Scansione dei file', source: 'Fonte', local: 'file locale',
        resolution: 'Risoluzione', vcodec: 'Video', acodec: 'Audio', channels: 'Canali', languages: 'Lingue',
        audioWarn: 'Audio {c}: il tuo browser non può decodificarlo. Il video andrà senza audio — aprilo in un lettore esterno (VLC / MX Player).',
        more: 'Altro',
        watchSearchPlaceholder: 'Cerca per titolo…',
        watchEmpty: 'Cerca qualcosa da vedere, ascoltare o leggere.', remove: 'Rimuovi', other: 'Altro', home: 'Home', watchedDone: 'Visto', markDone: 'Segna come visto', markUndone: 'Segna come da vedere',
        game: 'Gioco', continueW: 'Continua a guardare', newlyAdded: 'Novità', preview: 'Anteprima', send: 'Invia', addManual: 'Aggiungi manualmente',
        manualOnly: 'Senza ricerca online — aggiunto manualmente', addAsType: 'Aggiungi come {type}',
        season: 'Stagione', otherEps: 'Altri', mArtist: 'Artista', mAuthor: 'Autore', mSeries: 'Serie', mAlbum: 'Album', mCollection: 'Raccolta',
        changeFolder: 'Cambia cartella', exportDb: 'Esporta database', importDb: 'Importa database',
        folderPrompt: 'Percorso della cartella da scansionare:', importConfirm: 'Riavvia Gobby per caricare il database importato.',
        libraries: 'Librerie', view: 'Vedi', viewing: 'In uso', rebind: 'Usa questa cartella', rebindHint: 'Ri-collega questa libreria alla cartella in cui si trova Gobby ora', rebindConfirm: 'Ri-collegare questa libreria alla cartella attuale di Gobby?', unreachable: 'non accessibile', unavailable: 'Non disponibile', unavailableNote: 'Questo elemento non è accessibile al momento (file spostato o unità disconnessa).', importMerged: 'Importate {n} libreria/e.', importNone: 'Niente di nuovo da importare.',
        offlineTitle: 'Gobby è tornato a casa', offlineSub: 'Il piccolo elfo ha spento il server. Riapri Gobby per continuare. 🧦',
        clearFilters: 'Pulisci filtri',
        connect: 'Connetti dispositivi',
        devices: 'Dispositivi connessi', thisDevice: 'questo dispositivo', noDevices: 'Ancora nessun dispositivo.', now: 'ora',
        changeCover: 'Cambia copertina', coverFromUrl: 'Da URL', coverUrlPrompt: 'Incolla l’URL dell’immagine:', openTab: 'Apri in scheda', castTo: 'Trasmetti alla TV', moreActions: 'Altro', castStop: 'Ferma', castFailed: 'Impossibile trasmettere. Chromecast richiede HTTP e un formato compatibile (mp4).', continueAt: 'Fino a:', resumeHere: 'Riprendi',
        customFields: 'Campi', addField: 'Aggiungi campo', fieldName: 'Nome', fieldValue: 'Valore',
        tunnelStart: 'Apri tunnel pubblico', tunnelStop: 'Chiudi tunnel', tunnelStarting: 'Preparazione del tunnel…', tabLocal: 'Rete locale', tabInternet: 'Internet',
        mcpConnect: 'Connetti MCP', mcpIntro: 'Gobby espone un server MCP perché Claude possa cercare nella tua libreria e gestire la lista da vedere. Aggiungilo in uno di questi modi:',
        copy: 'Copia', mcpUrl: 'URL del server', mcpCli: 'Con la CLI di Claude Code', mcpTools: 'Strumenti: search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'Info', aboutDeps: 'Dipendenze', aboutApis: 'API e servizi',
        updCheck: 'Cerca aggiornamenti', updChecking: 'Controllo…', updLatest: 'Hai la versione più recente', updNew: 'Nuova versione: {v}', updApply: 'Aggiorna Gobby', updApplying: 'Aggiornamento…', updDone: 'Aggiornato — riavvia Gobby per usare la nuova versione.', updFail: 'Aggiornamento non riuscito',
        dServer: 'server', dUI: 'interfaccia', dSound: 'suono', dMcp: 'server MCP', dDb: 'database', dTags: 'metadati audio', dQr: 'codici QR', dFfmpeg: 'video, audio e sottotitoli', dEpub: 'lettore epub', dZip: 'lettura zip',
        aMovies: 'film e serie', aBooks: 'libri', aMusic: 'musica', aMusicCovers: 'copertine musica', aGames: 'giochi', aFallback: 'copertine di riserva', aRemote: 'accesso remoto', aCast: 'trasmetti in TV',
      },
      pt: {
        search: 'Pesquisar na sua biblioteca…', series: 'Séries', movie: 'Filmes',
        audio: 'Música', book: 'Livros', files: 'Ficheiros', watch: 'Para ver',
        albums: 'Álbuns', authors: 'Autores', movies: 'Filmes', singles: 'Soltos', folders: 'Pastas', looseFiles: 'Soltos', filesUnit: 'ficheiros', filterFiles: 'Filtrar ficheiros…',
        empty: 'Ainda não há nada aqui.', episodes: 'episódios', tracks: 'faixas', books: 'livros',
        title: 'Título', notes: 'Notas', rating: 'Avaliação', location: 'Localização', reveal: 'Abrir pasta', uploadHere: 'Enviar aqui', deleteFile: 'Apagar do disco', deleteConfirm: 'Apagar «{name}» do disco? Não pode ser desfeito.', deleteFailed: 'Não foi possível apagar.',
        director: 'Realização', cast: 'Elenco', back: 'Voltar', add: 'Adicionar',
        enrichThemes: 'Procurar capas', theme: 'Mudar tema', fetching: 'A procurar capas', sound: 'Som', language: 'Idioma',
        forceAll: 'Procurar tudo (substitui)', stop: 'Parar',
        identify: 'ID (IMDb, ex. tt1375666)', update: 'Atualizar', updating: 'A atualizar…', editId: 'Editar ID', openRef: 'Ver referência',
        section: 'Secção', autoSection: 'Automática', movieSection: 'Filmes', seriesSection: 'Séries', musicSection: 'Música', bookSection: 'Livros', filesSection: 'Ficheiros',
        download: 'Transferir',
        filters: 'Filtros', fFormat: 'Formato', fYear: 'Ano',
        fCover: 'Capa', fCoverAll: 'Todas', fCoverWith: 'Com capa', fCoverWithout: 'Sem capa',
        fRating: 'Avaliação mín.', fAny: 'Qualquer', fGenre: 'Género', clear: 'Limpar',
        openExternal: 'Copiar ligação', openWith: 'Abrir com…',
        copied: 'Copiado', openStream: 'Abrir', playHere: 'Reproduzir aqui', subsOff: 'Sem legendas',
        subStyle: 'Legendas', subSize: 'Tamanho', subColor: 'Cor', subBox: 'Fundo', on: 'Sim', off: 'Não', playerSettings: 'Áudio e legendas',
        scanning: 'A analisar ficheiros', source: 'Fonte', local: 'ficheiro local',
        resolution: 'Resolução', vcodec: 'Vídeo', acodec: 'Áudio', channels: 'Canais', languages: 'Idiomas',
        audioWarn: 'Áudio {c}: o seu navegador não o consegue descodificar. O vídeo irá sem som — abra-o num leitor externo (VLC / MX Player).',
        more: 'Mais',
        watchSearchPlaceholder: 'Pesquisar por título…',
        watchEmpty: 'Procure algo para ver, ouvir ou ler.', remove: 'Remover', other: 'Outro', home: 'Início', watchedDone: 'Visto', markDone: 'Marcar como visto', markUndone: 'Marcar como pendente',
        game: 'Jogo', continueW: 'Continuar a ver', newlyAdded: 'Novidades', preview: 'Pré-visualizar', send: 'Enviar', addManual: 'Adicionar manualmente',
        manualOnly: 'Sem pesquisa online — adicionado manualmente', addAsType: 'Adicionar como {type}',
        season: 'Temporada', otherEps: 'Outros', mArtist: 'Artista', mAuthor: 'Autor', mSeries: 'Série', mAlbum: 'Álbum', mCollection: 'Coleção',
        changeFolder: 'Mudar pasta', exportDb: 'Exportar base de dados', importDb: 'Importar base de dados',
        folderPrompt: 'Caminho da pasta a analisar:', importConfirm: 'Reinicie o Gobby para carregar a base importada.',
        libraries: 'Bibliotecas', view: 'Ver', viewing: 'A ver', rebind: 'Usar esta pasta', rebindHint: 'Voltar a associar esta biblioteca à pasta onde o Gobby está agora', rebindConfirm: 'Voltar a associar esta biblioteca à pasta atual do Gobby?', unreachable: 'inacessível', unavailable: 'Indisponível', unavailableNote: 'Este item não está acessível de momento (ficheiro movido ou unidade desligada).', importMerged: 'Importadas {n} biblioteca(s).', importNone: 'Nada de novo para importar.',
        offlineTitle: 'O Gobby foi para casa', offlineSub: 'O pequeno elfo desligou o servidor. Volta a abrir o Gobby para continuar. 🧦',
        clearFilters: 'Limpar filtros',
        connect: 'Ligar dispositivos',
        devices: 'Dispositivos ligados', thisDevice: 'este dispositivo', noDevices: 'Ainda sem dispositivos.', now: 'agora',
        changeCover: 'Mudar capa', coverFromUrl: 'A partir de URL', coverUrlPrompt: 'Cole o URL da imagem:', openTab: 'Abrir em separador', castTo: 'Transmitir para a TV', moreActions: 'Mais', castStop: 'Parar', castFailed: 'Não foi possível transmitir. O Chromecast precisa de HTTP e formato compatível (mp4).', continueAt: 'Até:', resumeHere: 'Retomar',
        customFields: 'Campos', addField: 'Adicionar campo', fieldName: 'Nome', fieldValue: 'Valor',
        tunnelStart: 'Abrir túnel público', tunnelStop: 'Fechar túnel', tunnelStarting: 'A preparar o túnel…', tabLocal: 'Rede local', tabInternet: 'Internet',
        mcpConnect: 'Ligar MCP', mcpIntro: 'O Gobby expõe um servidor MCP para que o Claude possa pesquisar na sua biblioteca e gerir a lista para ver. Adicione-o de uma destas formas:',
        copy: 'Copiar', mcpUrl: 'URL do servidor', mcpCli: 'Com a CLI do Claude Code', mcpTools: 'Ferramentas: search_media, get_media, play_media, add_to_watchlist, list_watchlist, library_info.',
        about: 'Acerca', aboutDeps: 'Dependências', aboutApis: 'APIs e serviços',
        updCheck: 'Procurar atualização', updChecking: 'A verificar…', updLatest: 'Tem a versão mais recente', updNew: 'Nova versão: {v}', updApply: 'Atualizar o Gobby', updApplying: 'A atualizar…', updDone: 'Atualizado — reinicie o Gobby para usar a nova versão.', updFail: 'Falha na atualização',
        dServer: 'servidor', dUI: 'interface', dSound: 'som', dMcp: 'servidor MCP', dDb: 'base de dados', dTags: 'metadados de áudio', dQr: 'códigos QR', dFfmpeg: 'vídeo, áudio e legendas', dEpub: 'leitor epub', dZip: 'leitura zip',
        aMovies: 'filmes e séries', aBooks: 'livros', aMusic: 'música', aMusicCovers: 'capas de música', aGames: 'jogos', aFallback: 'capas alternativas', aRemote: 'acesso remoto', aCast: 'transmitir para TV',
      },
    },
    // supported languages; English is the fallback for any missing key
    langs: ['es', 'en', 'fr', 'de', 'it', 'pt'],
    t(key) { return (this.strings[this.lang] && this.strings[this.lang][key]) || this.strings.en[key] || key; },

    get top() { return this.stack[this.stack.length - 1]; },

    init() {
      this.theme = localStorage.getItem('gobby-theme') || 'dark';
      document.documentElement.setAttribute('data-theme', this.theme);
      // First visit: honor the browser's language if we speak it, else Spanish.
      const nav = (navigator.language || '').slice(0, 2).toLowerCase();
      this.lang = localStorage.getItem('gobby-lang') || (this.langs.includes(nav) ? nav : 'es');
      window.addEventListener('popstate', () => {
        if (this.stack.length > 1) this.stack.pop();
      });
      this.loadInfo();
      this.loadHome();
      this.pollProgress(); // pick up a scan/enrich already running at startup
      this.initCast();
      this.initSound();
      this.loadSubCfg();
      this.openFromHash(); // deep-link: #item/<id> opens that item on load
      window.addEventListener('hashchange', () => this.openFromHash());
      window.addEventListener('keydown', (e) => this._onPlayerKey(e));
      document.addEventListener('click', (e) => this._onDocClick(e));
      this.auraNeutral();
      this.watchBackend();
    },

    // A cached page keeps working after the server dies, which is misleading —
    // clicks do nothing and playback 404s. Ping the backend; when it's gone, throw
    // up a blocking "Gobby left" curtain (once, with the error cue) until it's back.
    offline: false,
    offlinePoke: false,
    watchBackend() {
      const ping = async () => {
        try {
          const r = await fetch('/api/info', { cache: 'no-store' });
          if (!r.ok) throw 0;
          if (this.offline) { this.offline = false; this.loadInfo(); } // came back
        } catch (e) {
          this.offline = true;
        }
      };
      setInterval(ping, 4000);
    },
    // wobble + error cue only when the user pokes the logo, not on its own
    pokeOffline() {
      if (this.soundOn && window.cuelume) window.cuelume.play('error');
      this.offlinePoke = true;
      clearTimeout(this._pokeT);
      this._pokeT = setTimeout(() => { this.offlinePoke = false; }, 700);
    },
    // Open the item named in the URL hash (#item/<id>), so a shared link lands
    // directly on it. Silently ignores a stale/unknown id.
    async openFromHash() {
      const m = location.hash.match(/^#item\/(\d+)$/);
      if (!m) return;
      if (this.top.view === 'item' && this.top.item && String(this.top.item.id) === m[1]) return;
      const r = await fetch('/api/item/' + m[1]);
      // pushView (not stack.push) so a deep-linked item gets a history entry to pop
      if (r.ok) this.pushView({ view: 'item', item: await r.json() });
    },

    // ---- UI sound feedback (cuelume, vendored — synthesized, no assets) ----
    soundOn: true,
    initSound() {
      this.soundOn = localStorage.getItem('gobby-sound') !== 'off';
      const cl = window.cuelume;
      if (!cl) return;
      cl.setEnabled(this.soundOn);
      // cuelume lazily creates its AudioContext on first use. Sound plays on
      // click only (no hover blip) so each action's cue is heard cleanly.
      const sel = 'button, a, .item, .tv-row, [role=button]';
      const clickMap = [
        ['.brand', 'loading'],                    // Gobby logo/name
        ['.back-btn', 'error'], ['.search-clear', 'error'], ['.tunnel-btn.stop', 'error'], ['.wfield-del', 'error'], // back / cancel
        ['nav.tabs button', 'pulse'],             // main tabs
        ['[data-enrich]', 'scan'],                // fetch covers
        ['.menu button', 'release'],              // overflow menu items
        ['.item', 'droplet'], ['.tv-row', 'page'], ['.action', 'bloom'], ['.chip', 'sparkle'],
      ];
      const pick = (el, map, dflt) => { for (const [q, s] of map) if (el.closest(q)) return s; return dflt; };
      // One sound per gesture: 'click' fires once per tap (unlike pointerdown,
      // which some devices emit twice for touch+mouse), and a short guard drops
      // any stray duplicate so cues never overlap.
      let lastPlay = 0;
      document.addEventListener('click', (e) => {
        const now = performance.now();
        if (now - lastPlay < 120) return;
        const el = e.target.closest(sel + ', input, select');
        if (el) { lastPlay = now; cl.play(pick(el, clickMap, 'tick')); }
      }, true);
      // Soft keystroke tick while typing in text fields (skip modifiers/navigation).
      let lastKey = 0;
      document.addEventListener('keydown', (e) => {
        const t = e.target;
        if (!t || !/^(INPUT|TEXTAREA)$/.test(t.tagName) || t.type === 'range') return;
        if (e.key.length !== 1 && e.key !== 'Backspace') return; // only real edits
        const now = performance.now();
        if (now - lastKey < 45) return;
        lastKey = now; cl.play('tick');
      }, true);
    },
    success() { if (window.cuelume) window.cuelume.play('success'); },
    toggleSound() {
      this.soundOn = !this.soundOn;
      localStorage.setItem('gobby-sound', this.soundOn ? 'on' : 'off');
      if (window.cuelume) { window.cuelume.setEnabled(this.soundOn); if (this.soundOn) window.cuelume.play('toggle'); }
    },
    async loadInfo() {
      const r = await fetch('/api/info');
      if (r.ok) {
        this.info = await r.json();
        this.sections = this.info.sections || {};
      }
    },
    // tabs with no content are hidden; watch/files always shown.
    visibleTabs() {
      const all = ['series', 'movie', 'audio', 'book', 'files', 'watch'];
      if (!this.sections || !Object.keys(this.sections).length) return all;
      return all.filter(t => t === 'watch' || t === 'files' || this.sections[t]);
    },

    // ---- connect devices (QR + connected list) ----
    connectOpen: false,
    connTab: 'local',
    connBase: '',
    clients: [],
    qrBust: 0,
    clientsTimer: null,
    async openConnect() {
      this.connectOpen = true;
      this.qrBust = Date.now();
      await this.loadClients();
      if (this.clientsTimer) clearInterval(this.clientsTimer);
      this.clientsTimer = setInterval(() => {
        if (!this.connectOpen) { clearInterval(this.clientsTimer); return; }
        this.loadClients();
      }, 3000);
    },
    async loadClients() {
      const r = await fetch('/api/clients');
      if (!r.ok) return;
      const v = await r.json();
      this.connBase = v.base || '';
      this.clients = (v.clients || []).sort((a, b) => b.last_at - a.last_at);
      this.loadTunnel();
    },

    // ---- public Cloudflare tunnel ----
    tun: { running: false, url: '', ready: false, error: '' },
    tunBusy: false,
    tunTimer: null,
    async loadTunnel() {
      const r = await fetch('/api/tunnel/status');
      if (r.ok) this.tun = await r.json();
      // "busy" = starting up or waiting for Cloudflare's edge to route
      this.tunBusy = this.tun.running && !this.tun.ready;
    },
    // Poll status until the tunnel is ready or errors — downloading cloudflared +
    // the edge routing to us takes ~15-90s, and a single status read (the old bug)
    // left the spinner stuck forever.
    pollTunnel() {
      clearTimeout(this.tunTimer);
      const tick = async () => {
        await this.loadTunnel();
        if (this.tun.running && !this.tun.ready && !this.tun.error) {
          this.tunTimer = setTimeout(tick, 2000);
        }
      };
      tick();
    },
    async startTunnel() {
      this.tunBusy = true;
      this.tun = { running: true, url: '', ready: false, error: '' };
      await fetch('/api/tunnel/start', { method: 'POST' });
      this.pollTunnel();
    },
    async stopTunnel() {
      clearTimeout(this.tunTimer);
      await fetch('/api/tunnel/stop', { method: 'POST' });
      this.tunBusy = false;
      this.loadTunnel();
    },
    deviceIcon(ua) {
      if (/mobi|android|iphone|ipad/i.test(ua)) return 'smartphone';
      if (/tv|smarttv|tizen|webos/i.test(ua)) return 'monitor';
      return 'laptop';
    },
    deviceName(ua) {
      if (!ua) return '—';
      const os = /android/i.test(ua) ? 'Android' : /iphone|ipad|ios/i.test(ua) ? 'iOS'
        : /windows/i.test(ua) ? 'Windows' : /mac os|macintosh/i.test(ua) ? 'macOS'
        : /linux/i.test(ua) ? 'Linux' : '';
      const br = /edg/i.test(ua) ? 'Edge' : /chrome/i.test(ua) ? 'Chrome' : /firefox/i.test(ua) ? 'Firefox'
        : /safari/i.test(ua) ? 'Safari' : '';
      return [os, br].filter(Boolean).join(' · ') || ua.slice(0, 40);
    },
    agoLabel(ts) {
      const s = Math.max(0, Math.floor(Date.now() / 1000 - ts));
      if (s < 10) return this.t('now');
      if (s < 60) return s + 's';
      if (s < 3600) return Math.floor(s / 60) + 'm';
      return Math.floor(s / 3600) + 'h';
    },

    // ---- library actions ----
    async changeFolder() {
      const p = prompt(this.t('folderPrompt'), this.info.root || '');
      if (!p || !p.trim()) return;
      const r = await fetch('/api/library/folder', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: p.trim() }),
      });
      if (!r.ok) { alert(await r.text()); return; }
      // repoint + rescan happen server-side; reset the UI and poll for the rescan
      this.libOpen = false;
      this.stack = [{ view: 'home' }]; this.tab = 'home';
      this.loadInfo(); this.loadHome(); this.pollProgress();
    },
    exportDb() { window.location.href = '/api/db/export'; },
    importDb() { this.$refs.dbfile.click(); },
    async doImport(ev) {
      const f = ev.target.files[0];
      if (!f) return;
      const r = await fetch('/api/db/import', { method: 'POST', body: f });
      ev.target.value = '';
      if (!r.ok) { alert(await r.text()); return; }
      const res = await r.json();
      const n = (res.added || []).length;
      alert(n ? this.t('importMerged').replace('{n}', n) : this.t('importNone'));
      this.loadLibraries();
    },

    // library switcher
    libOpen: false,
    libraries: [],
    async loadLibraries() {
      try { this.libraries = await (await fetch('/api/libraries')).json() || []; } catch (e) { this.libraries = []; }
    },
    openLibraries() { this.libOpen = true; this.loadLibraries(); },
    async switchLibrary(key) {
      const r = await fetch('/api/library/switch', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key }),
      });
      if (!r.ok) { alert(await r.text()); return; }
      this.libOpen = false;
      this.stack = [{ view: 'home' }]; this.tab = 'home';
      this.loadInfo(); this.loadHome(); this.pollProgress();
    },
    async rebindLibrary(key) {
      if (!confirm(this.t('rebindConfirm'))) return;
      const r = await fetch('/api/library/rebind', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key }),
      });
      if (!r.ok) { alert(await r.text()); return; }
      this.libOpen = false;
      this.stack = [{ view: 'home' }]; this.tab = 'home';
      this.loadInfo(); this.loadHome(); this.pollProgress();
    },

    toggleTheme() {
      this.theme = this.theme === 'dark' ? 'light' : 'dark';
      localStorage.setItem('gobby-theme', this.theme);
      document.documentElement.setAttribute('data-theme', this.theme);
    },
    langOpen: false,
    langNames: { es: 'Español', en: 'English', fr: 'Français', de: 'Deutsch', it: 'Italiano', pt: 'Português' },
    langName(l) { return this.langNames[l] || l.toUpperCase(); },
    setLang(l) {
      this.lang = l;
      localStorage.setItem('gobby-lang', l);
    },
    cycleLang() {
      const i = this.langs.indexOf(this.lang);
      this.setLang(this.langs[(i + 1) % this.langs.length]);
    },

    svg(name) {
      const p = {
        search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
        mic: '<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 10a7 7 0 0 0 14 0M12 17v5M8 22h8"/>',
        sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M6.3 17.7l-1.4 1.4M19.1 4.9l-1.4 1.4"/>',
        moon: '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
        palette: '<circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.9 0 1.5-.7 1.5-1.5 0-.4-.2-.8-.4-1-.3-.3-.4-.6-.4-1 0-.8.7-1.5 1.5-1.5H16c3.3 0 6-2.7 6-6 0-4.9-4.5-9-10-9Z"/>',
        film: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M7 3v18M17 3v18M3 7.5h4M17 7.5h4M3 12h18M3 16.5h4M17 16.5h4"/>',
        clapper: '<path d="m4 11 16-3M4 11l-1 8h18l-1-8M4 11 3 6l4-.8M9.3 5.2l4-.8M15.3 4.2l4-.8"/>',
        music: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
        book: '<path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20"/>',
        bookmark: '<path d="m19 21-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2Z"/>',
        back: '<path d="m12 19-7-7 7-7M19 12H5"/>',
        check: '<path d="M20 6 9 17l-5-5"/>',
        close: '<path d="M18 6 6 18M6 6l12 12"/>',
        gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/>',
        refresh: '<path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5"/>',
        folder: '<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9L9.6 3.9A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
        filter: '<path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z"/>',
        play: '<path d="m6 3 14 9-14 9V3Z"/>',
        pause: '<rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>',
        fullscreen: '<path d="M8 3H5a2 2 0 0 0-2 2v3M21 8V5a2 2 0 0 0-2-2h-3M16 21h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/>',
        copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
        download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
        external: '<path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
        monitor: '<rect width="20" height="14" x="2" y="3" rx="2"/><path d="M8 21h8M12 17v4"/>',
        volume: '<path d="M11 5 6 9H2v6h4l5 4V5Z"/><path d="M15.5 8.5a5 5 0 0 1 0 7M19 5a9 9 0 0 1 0 14"/>',
        globe: '<circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15 15 0 0 1 0 20 15 15 0 0 1 0-20Z"/>',
        languages: '<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/>',
        info: '<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>',
        alert: '<path d="M10.3 3.3 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.3a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/>',
        clock: '<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
        users: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8"/>',
        more: '<circle cx="12" cy="5" r="1.6" fill="currentColor"/><circle cx="12" cy="12" r="1.6" fill="currentColor"/><circle cx="12" cy="19" r="1.6" fill="currentColor"/>',
        home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V20a1 1 0 0 0 1 1h4v-6h4v6h4a1 1 0 0 0 1-1V9.5"/>',
        tag: '<path d="M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0l-6.2-6.2a2 2 0 0 1-.6-1.4V5a2 2 0 0 1 2-2h4a2 2 0 0 1 1.4.6l7.4 7.4a2 2 0 0 1 0 2.8Z"/><circle cx="8" cy="8" r="1" fill="currentColor"/>',
        star: '<path d="m12 2 3.1 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.8 21l1.2-6.8-5-4.9 6.9-1Z"/>',
        layers: '<path d="m12 2 9 5-9 5-9-5 9-5Z"/><path d="m3 12 9 5 9-5M3 17l9 5 9-5"/>',
        trash: '<path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"/>',
        plus: '<path d="M12 5v14M5 12h14"/>',
        gamepad: '<path d="M6 12h4M8 10v4M15 11h.01M18 13h.01"/><rect width="20" height="12" x="2" y="6" rx="6"/>',
        stop: '<rect width="12" height="12" x="6" y="6" rx="2" fill="currentColor" stroke="none"/>',
        share: '<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><path d="m8.6 13.5 6.8 4M15.4 6.5l-6.8 4"/>',
        smartphone: '<rect width="14" height="20" x="5" y="2" rx="2"/><path d="M12 18h.01"/>',
        laptop: '<rect width="18" height="12" x="3" y="4" rx="2"/><path d="M2 20h20"/>',
        cast: '<path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6"/><path d="M2 12a9 9 0 0 1 8 8M2 16a5 5 0 0 1 4 4"/><path d="M2 20h.01"/>',
        plug: '<path d="M12 22v-5M9 8V2M15 8V2M7 8h10v3a5 5 0 0 1-10 0V8Z"/>',
        file: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6"/>',
        chevron: '<path d="m9 18 6-6-6-6"/>',
        mute: '<path d="M11 5 6 9H2v6h4l5 4V5Z"/><path d="m22 9-6 6M16 9l6 6"/>',
        github: '<path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.9a3.4 3.4 0 0 0-.9-2.6c3-.3 6.1-1.5 6.1-6.6a5.1 5.1 0 0 0-1.4-3.5 4.8 4.8 0 0 0-.1-3.5s-1.1-.3-3.7 1.4a12.6 12.6 0 0 0-6.6 0C6.3 1.6 5.2 1.9 5.2 1.9a4.8 4.8 0 0 0-.1 3.5A5.1 5.1 0 0 0 3.7 9c0 5 3.1 6.3 6.1 6.6a3.4 3.4 0 0 0-.9 2.6V22"/>',
        edit: '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
      }[name] || '';
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="ic">${p}</svg>`;
    },

    tabIcon(tab) {
      return this.svg({ home: 'home', series: 'film', movie: 'clapper', audio: 'music', book: 'book', files: 'folder', watch: 'bookmark' }[tab] || 'film');
    },
    mediaIcon(section) {
      const key = section || this.tab;
      return this.svg({ series: 'film', movie: 'clapper', audio: 'music', music: 'music', book: 'book', files: 'folder' }[key] || 'film');
    },
    // label for the artist/album fields adapts to the media kind
    metaLabel(field, kind) {
      if (field === 'artist') return this.t({ video: 'director', audio: 'mArtist', book: 'mAuthor' }[kind] || 'mArtist');
      return this.t({ video: 'mSeries', audio: 'mAlbum', book: 'mCollection' }[kind] || 'mAlbum');
    },
    idLabel(kind) {
      return { video: 'IMDb ID (ej. tt1375666)', book: 'Open Library cover ID', audio: 'MusicBrainz release-group ID' }[kind] || 'ID';
    },
    groupLabel() { return this.t({ series: 'series', audio: 'albums', book: 'authors', files: 'folders' }[this.tab] || 'series'); },
    looseLabel() { return this.t({ movie: 'movies', audio: 'singles', book: 'books', files: 'looseFiles' }[this.tab] || 'singles'); },
    groupUnit() { return this.t({ series: 'episodes', audio: 'tracks', book: 'books', files: 'filesUnit' }[this.tab] || 'episodes'); },
    // format filter options depend on the media kind of the current tab
    // Years present in the library, newest first. Falls back to a sensible recent
    // range if the library reports none yet.
    yearOptions() {
      const now = new Date().getFullYear();
      let hi = this.info.yearMax || now;
      let lo = this.info.yearMin || (now - 40);
      if (hi < lo) hi = lo;
      const out = [];
      for (let y = hi; y >= lo; y--) out.push(y);
      return out;
    },
    formatOptions() {
      return {
        series: ['mkv', 'mp4', 'avi'], movie: ['mkv', 'mp4', 'avi', 'm4v'],
        audio: ['flac', 'mp3', 'm4a', 'ogg', 'wav'], book: ['epub', 'pdf', 'mobi', 'cbz'],
      }[this.tab] || ['mkv', 'mp4', 'mp3', 'flac', 'pdf', 'epub'];
    },

    // ---- navigation ----
    // Home = first content tab (series → movie → …), stack reset.
    goHome() { this.goTab('home'); },
    goTab(tab) {
      this.tab = tab;
      this.auraNeutral();
      this.stack = [{ view: 'home' }];
      // strip a residual #item hash, else back onto this entry reopens the old item
      history.replaceState({ gobby: true }, '', location.pathname + location.search);
      if (tab === 'watch') this.loadWatch();
      else if (tab === 'home') this.loadHome();
      else if (tab === 'files') this.goFiles();
      else this.load();
    },

    async goFiles() { this.loadTree(); },

    // ---- size tree (treeview + treemap) ----
    tree: null,          // root TreeNode from /api/tree
    treeOpen: {},        // path -> bool (expanded folders)
    async loadTree() {
      const r = await fetch('/api/tree');
      this.tree = r.ok ? await r.json() : null;
    },
    toggleNode(p) { this.treeOpen[p] = !this.treeOpen[p]; },
    treeQ: '',
    _treeMatch(n, q) {
      if ((n.name || '').toLowerCase().includes(q)) return true;
      return (n.children || []).some(c => this._treeMatch(c, q));
    },
    treeRows() {
      const out = [];
      const q = this.treeQ.trim().toLowerCase();
      const walk = (n, depth) => {
        const kids = (n.children || []);
        const max = kids.reduce((m, c) => Math.max(m, c.size), 0) || 1;
        for (const c of kids) {
          if (q && !this._treeMatch(c, q)) continue;
          out.push({ n: c, depth, siblingMax: max });
          if (c.dir && (q || this.treeOpen[c.path])) walk(c, depth + 1);
        }
      };
      if (this.tree) walk(this.tree, 0);
      return out;
    },
    fmtSize(b) {
      if (!b) return '0 B';
      const u = ['B', 'KB', 'MB', 'GB', 'TB'];
      let i = 0, v = b;
      while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
      return (v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)) + ' ' + u[i];
    },
    // Bar width = node size vs its largest sibling (carried on the row), so the
    // heaviest entry at each level fills the bar and the rest scale down.
    treeBarPct(row) { return Math.max(2, (row.n.size / row.siblingMax) * 100); },
    // Color by content section, so a folder of movies reads the same blue as the
    // Movies tab. Folders inherit their dominant section from the backend.
    sectionColor(section) {
      return { movie: '#3b82f6', series: '#8b5cf6', music: '#10b981', book: '#f59e0b', files: '#64748b' }[section] || '#64748b';
    },
    treeTotal() { return this.tree ? this.fmtSize(this.tree.size) : '—'; },
    treeCount() { return this.tree ? this.tree.count : 0; },
    openTreeLeaf(n) { if (n.id) this.openItem({ id: n.id }); },
    // ---- Files: reveal + upload (host-only; buttons hidden unless info.local) ----
    ctx: { open: false, x: 0, y: 0, node: {} },
    dropPath: '',
    _uploadDir: '',
    async reveal(path) {
      await fetch('/api/reveal', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path }),
      }).catch(() => {});
    },
    treeMenu(e, node) {
      this.ctx = { open: true, x: e.clientX, y: e.clientY, node };
    },
    pickUpload(dir) { this._uploadDir = dir; this.$refs.uploadInput.click(); },
    doUpload(e) {
      const f = e.target.files && e.target.files[0];
      if (f) this._sendUpload(this._uploadDir, f);
      e.target.value = '';
    },
    dropUpload(e, dir) {
      this.dropPath = '';
      const f = e.dataTransfer.files && e.dataTransfer.files[0];
      if (f) this._sendUpload(dir, f);
    },
    async _sendUpload(dir, file) {
      const fd = new FormData();
      fd.append('dir', dir);
      fd.append('file', file);
      const r = await fetch('/api/upload', { method: 'POST', body: fd }).catch(() => null);
      if (r && r.ok) setTimeout(() => this.loadTree(), 800);
    },
    // Treemap strips for the top level: width ∝ size, color by content section.
    treemap() {
      const kids = (this.tree && this.tree.children) || [];
      const total = kids.reduce((s, k) => s + k.size, 0) || 1;
      return kids.filter(k => k.size > 0).map(k => ({
        name: k.name, size: k.size, w: (k.size / total) * 100,
        color: this.sectionColor(k.section), dir: k.dir, path: k.path, node: k,
      }));
    },

    // group a series' episodes by season → [{season, items}], seasons ascending,
    // episodes ordered within. Season 0 (or missing) is treated as "no season".
    seasonsOf(group) {
      if (!group || !group.items) return [];
      const map = {};
      for (const it of group.items) {
        const s = it.season || 0;
        (map[s] = map[s] || []).push(it);
      }
      return Object.keys(map)
        .map(Number).sort((a, b) => a - b)
        .map(s => ({ season: s, items: map[s].sort((a, b) => (a.episode || 0) - (b.episode || 0)) }));
    },
    hasSeasons(group) {
      const s = this.seasonsOf(group);
      return s.length > 1 || (s.length === 1 && s[0].season > 0);
    },
    activeSeason: null, // which season tab is shown (null = first)
    shownSeason(group) {
      const seasons = this.seasonsOf(group);
      if (!seasons.length) return null;
      const found = seasons.find(s => s.season === this.activeSeason);
      return found || seasons[0];
    },
    async loadHome() {
      const r = await fetch('/api/home');
      const v = r.ok ? await r.json() : {};
      // Collapse episodes of the same series into one card (the series), so a
      // shelf shows "Breaking Bad" once, not every episode.
      const dedup = (arr) => {
        const seen = new Set(), out = [];
        for (const it of arr || []) {
          const key = it.album ? it.section + '|' + it.album : 'id' + it.id;
          if (seen.has(key)) continue;
          seen.add(key); out.push(it);
        }
        return out;
      };
      this.homeCont = dedup(v.continue);
      this.homeNew = dedup(v.added);
    },
    pushView(v) {
      this.stack.push(v);
      // Deep-linkable URL for items so a link points at the exact thing. Other
      // views keep the bare URL (no meaningful shareable identity).
      const hash = v.view === 'item' && v.item ? '#item/' + v.item.id : '';
      history.pushState({ gobby: true }, '', hash || location.pathname);
    },
    back() {
      if (this.showFilters) { this.showFilters = false; return; }
      if (this.stack.length > 1) history.back();
    },

    page: 1,
    looseTotal: 0,
    pageSize: 60,
    pages() { return Math.max(1, Math.ceil(this.looseTotal / this.pageSize)); },
    async load(resetPage = true) {
      if (this.tab === 'files') { this.goFiles(); return; }
      if (this.tab === 'home') return; // home shelves filter client-side (see applyFilters)
      if (resetPage) this.page = 1;
      const fq = this.filterQS();
      const r = await fetch(`/api/browse?kind=${this.tab}&q=${encodeURIComponent(this.q)}&page=${this.page}${fq ? '&' + fq : ''}`);
      const v = (await r.json()) || {};
      this.groups = v.groups || [];
      this.loose = v.loose || [];
      this.looseTotal = v.loose_total || 0;
      this.pageSize = v.page_size || 60;
    },
    goPage(p) {
      if (p < 1 || p > this.pages()) return;
      this.page = p;
      this.load(false);
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },

    // Reactive accessor for the current series' remote meta (synopsis/cast/…),
    // filled async by openGroup — read live so it renders the moment it arrives.
    showMeta() { return (this.top.group && this.top.group.show && this.top.group.show.meta) || {}; },
    async openGroup(g) {
      // A group with a single item (a lone book/track/episode) has nothing to
      // choose on its collection page — skip straight to the item detail.
      if (g.items && g.items.length === 1) { this.openItem(g.items[0]); return; }
      this.groupID = '';
      this.editId = false;
      this.activeSeason = null;
      this.auraFromCover((g.items || []).find(it => it.has_cover || it.cover_id) || g.items[0]);
      this.pushView({ view: 'series', group: g });
      // rich meta for the show is stored on the first episode
      if (g.items[0]) {
        const r = await fetch(`/api/item/${g.items[0].id}`);
        if (r.ok) {
          g.show = await r.json();
          this.groupID = g.show.imdb_id || '';
        }
      }
    },

    async openItem(it) {
      this.previewing = false;
      this.moreOpen = false;
      this.closeEpub();
      this.stopVideo();
      this.editId = false; // starts locked so the id can't be edited by accident
      const r = await fetch(`/api/item/${it.id}`);
      const full = r.ok ? await r.json() : { ...it };
      this.auraFromCover(full);
      this.pushView({ view: 'item', item: full });
    },

    // ---- global search (header magnifier) ----
    searching() { return this.searchOpen && !!this.q; },
    toggleSearch() {
      this.searchOpen = !this.searchOpen;
      if (this.searchOpen) { this.$nextTick(() => this.$refs.searchInput && this.$refs.searchInput.focus()); }
      else { this.q = ''; this.searchResults = []; }
    },
    closeSearch() { this.searchOpen = false; this.q = ''; this.searchResults = []; },
    // click anywhere outside the search header AND outside the results view closes it.
    // Uses the click target's ancestry, so a click ON a result still opens it first.
    _onDocClick(e) {
      if (!this.searchOpen) return;
      if (e.target.closest('.search-wrap') || e.target.closest('.search-view')) return;
      this.closeSearch();
    },
    // ---- voice search (Web Speech API) ----
    get voiceSupported() { return 'webkitSpeechRecognition' in window || 'SpeechRecognition' in window; },
    listening: false,
    voiceSearch() {
      if (this.listening) { this._rec && this._rec.stop(); return; }
      const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
      if (!SR) return;
      const rec = this._rec = new SR();
      rec.lang = { es: 'es-ES', en: 'en-US', fr: 'fr-FR', de: 'de-DE', it: 'it-IT', pt: 'pt-PT' }[this.lang] || 'es-ES';
      rec.interimResults = true;
      rec.onstart = () => { this.listening = true; };
      rec.onend = () => { this.listening = false; };
      rec.onerror = () => { this.listening = false; };
      rec.onresult = (e) => {
        this.q = Array.from(e.results).map(r => r[0].transcript).join('');
        this.runSearch();
      };
      rec.start();
    },
    async runSearch() {
      const q = this.q.trim();
      if (!q) { this.searchResults = []; return; }
      const r = await fetch('/api/library/search?q=' + encodeURIComponent(q));
      this.searchResults = r.ok ? (await r.json()) || [] : [];
    },
    // A search hit: series/album → its collection page; anything else → the item.
    async openSearch(res) {
      this.searchOpen = false; this.q = ''; this.searchResults = [];
      if (res.group) {
        const kind = { series: 'series', music: 'audio', book: 'book' }[res.section] || 'audio';
        const r = await fetch(`/api/browse?kind=${kind}&q=${encodeURIComponent(res.group)}`);
        const v = r.ok ? await r.json() : {};
        const g = (v.groups || []).find(x => x.name === res.group);
        if (g) { this.openGroup(g); return; }
      }
      this.openItem(res);
    },
    // Label under a result: section + year (or SxxExx / artist where it helps).
    searchSub(res) {
      const bits = [this.t(res.section === 'music' ? 'audio' : res.section) || res.section];
      if (res.group) bits.push(res.group);
      else if (res.artist) bits.push(res.artist);
      if (res.year) bits.push(res.year);
      return bits.filter(Boolean).join(' · ');
    },

    // SxxExx tag for a series episode (empty for movies/music/books)
    epTag(it) {
      if (!it || it.section !== 'series' || !it.episode) return '';
      return (it.season ? 'S' + String(it.season).padStart(2, '0') : '') + 'E' + String(it.episode).padStart(2, '0');
    },
    resumeEpId: 0, // episode to highlight when a series page opens from "continue"

    // Opening from a Home shelf: a grouped item (series/album/author) should open
    // the whole collection page, not a single loose episode. Loose items open
    // their detail directly.
    async openHome(it) {
      if (!it.album) { this.openItem(it); return; }
      this.resumeEpId = it.id;
      const kind = { series: 'series', music: 'audio', book: 'book', movie: 'movie' }[it.section] || 'series';
      const r = await fetch(`/api/browse?kind=${kind}&q=${encodeURIComponent(it.album)}`);
      const v = r.ok ? await r.json() : {};
      const g = (v.groups || []).find(x => x.name === it.album);
      if (g) this.openGroup(g);
      else this.openItem(it); // fell back (e.g. a standalone with an album tag)
    },

    async save(item) {
      await fetch(`/api/item/${item.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: item.title, notes: item.notes, rating: item.rating,
          year: parseInt(item.year) || 0, artist: item.artist || '',
          album: item.album || '', genre: item.genre || '',
        }),
      });
    },

    async setSection(item, section) {
      const r = await fetch(`/api/item/${item.id}/section`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ section }),
      });
      if (!r.ok) return;
      const refreshed = await fetch(`/api/item/${item.id}`);
      if (refreshed.ok) Object.assign(item, await refreshed.json());
    },

    imdbURL(item) { return /^tt\d+/.test(item.imdb_id || '') ? `https://www.imdb.com/title/${item.imdb_id}/` : ''; },
    // External reference URL for whichever id a kind uses (IMDb / Open Library /
    // MusicBrainz). Empty when there's no id, so the "open reference" button hides.
    refURL(kind, id) {
      id = (id || '').trim();
      if (!id) return '';
      // Books: the stored id is Open Library's cover_i (a cover image id, not an
      // OLID), so link to that cover image — the only stable URL it addresses.
      if (kind === 'book') return /^\d+$/.test(id) ? `https://covers.openlibrary.org/b/id/${id}-L.jpg` : `https://openlibrary.org/search?q=${encodeURIComponent(id)}`;
      if (kind === 'audio') return `https://musicbrainz.org/release-group/${id}`;
      return /^tt\d+/.test(id) ? `https://www.imdb.com/title/${id}/` : '';
    },
    editId: false, // whether the id input is unlocked for editing (pencil toggle)
    canShare() { return !!navigator.share; },

    // ---- MCP connection help ----
    mcpOpen: false,
    aboutOpen: false,
    // self-update state: idle → checking → 'latest' | {v} available → applying → done|error
    upd: { state: 'idle', latest: '', msg: '' },
    async checkUpdate() {
      this.upd = { state: 'checking', latest: '', msg: '' };
      try {
        const r = await (await fetch('/api/update/check')).json();
        if (r.error) { this.upd = { state: 'error', latest: '', msg: r.error }; return; }
        this.upd = r.available
          ? { state: 'available', latest: r.latest, msg: '' }
          : { state: 'latest', latest: r.latest, msg: '' };
      } catch (e) {
        this.upd = { state: 'error', latest: '', msg: String(e) };
      }
    },
    async applyUpdate() {
      this.upd = { ...this.upd, state: 'applying' };
      try {
        const res = await fetch('/api/update/apply', { method: 'POST' });
        const r = await res.json();
        this.upd = res.ok
          ? { state: 'done', latest: this.upd.latest, msg: '' }
          : { state: 'error', latest: '', msg: r.error || this.t('updFail') };
      } catch (e) {
        this.upd = { state: 'error', latest: '', msg: String(e) };
      }
    },
    mcpCopied: '',
    mcpURL() { return (this.info.base || '') + '/mcp'; },
    mcpCli() { return `claude mcp add --transport http gobby ${this.mcpURL()}`; },
    mcpJson() {
      return JSON.stringify({ mcpServers: { gobby: { type: 'http', url: this.mcpURL() } } }, null, 2);
    },
    async copyText(text, tag) {
      try {
        if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
        else { const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove(); }
        this.mcpCopied = tag || 'x'; setTimeout(() => this.mcpCopied = '', 1500);
      } catch (e) {}
    },

    // ---- Google Cast (send to Chromecast / Android TV / Google TV) ----
    castReady: false,
    initCast() {
      // the SDK calls this global once loaded; poll a few times in case of timing
      const setup = () => {
        if (!(window.cast && window.chrome && chrome.cast)) return false;
        cast.framework.CastContext.getInstance().setOptions({
          receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
          autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
        });
        this.castReady = true;
        return true;
      };
      window.__onGCastApiAvailable = (ok) => { if (ok) setup(); };
      let tries = 0;
      const t = setInterval(() => { if (setup() || ++tries > 20) clearInterval(t); }, 500);
    },
    canCast() { return this.castReady; },
    // load the item's stream on the selected TV. mime guides the receiver's player.
    async cast(item) {
      try {
        const ctx = cast.framework.CastContext.getInstance();
        await ctx.requestSession();
        const session = ctx.getCurrentSession();
        if (!session) return;
        let url = this.absStream(item);
        if (url.startsWith('https:')) url = url.replace(/^https:\/\/([^:/]+):(\d+)/, (m, h, p) => `http://${h}:${+p - 1}`);
        const mime = { video: 'video/mp4', audio: 'audio/mpeg' }[item.kind] || 'video/mp4';
        const mediaInfo = new chrome.cast.media.MediaInfo(url, mime);
        mediaInfo.metadata = new chrome.cast.media.GenericMediaMetadata();
        mediaInfo.metadata.title = item.title || '';
        await session.loadMedia(new chrome.cast.media.LoadRequest(mediaInfo));
        this.casting = true;
        this.markOpened(item);
      } catch (e) {
        this.casting = false;
        if (e && e.code && e.code !== 'cancel') alert(this.t('castFailed'));
      }
    },
    casting: false,
    stopCast() {
      try {
        const ctx = cast.framework.CastContext.getInstance();
        ctx.endCurrentSession(true);
      } catch (e) {}
      this.casting = false;
    },
    // native share sheet; tunnel deep-link (with key) if public, else LAN stream
    async share(item) {
      const url = this.shareURL(item);
      try { await navigator.share({ title: item.title, url }); this.markOpened(item); }
      catch (e) { this.copyText(url, 'share'); }
    },
    shareURL(item) {
      if (this.tun && this.tun.url) return this.tun.url + '#item/' + item.id;
      return this.absStream(item);
    },
    // what the browser can render inline in the detail page, by extension
    ext(item) { const m = (item.rel_path || '').match(/\.([^.]+)$/); return m ? m[1].toLowerCase() : ''; },
    previewType(item) {
      const e = this.ext(item);
      if (e === 'pdf') return 'pdf';
      if (e === 'epub') return 'epub';
      if (['mp3', 'm4a', 'ogg', 'wav', 'flac', 'aac'].includes(e)) return 'audio';
      if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp', 'svg'].includes(e)) return 'image';
      return '';
    },
    previewing: false,
    moreOpen: false,
    togglePreview() {
      this.previewing = !this.previewing;
      if (this.previewing && this.previewType(this.top.item) === 'epub') {
        this.$nextTick(() => this.openEpub(this.top.item));
      } else {
        this.closeEpub();
      }
    },
    // ---- epub reader (epub.js + jszip, vendored, loaded on demand) ----
    epubLoading: false,
    _epubScriptsLoaded: false,
    async _loadEpubLibs() {
      if (this._epubScriptsLoaded) return;
      const add = (src) => new Promise((ok, err) => {
        const s = document.createElement('script');
        s.src = src; s.onload = ok; s.onerror = err;
        document.head.appendChild(s);
      });
      await add('/jszip.min.js');
      await add('/epub.min.js');
      this._epubScriptsLoaded = true;
    },
    async openEpub(item) {
      this.closeEpub();
      this.epubLoading = true;
      try {
        await this._loadEpubLibs();
        // Fetch as ArrayBuffer: the stream URL has no .epub extension, so epub.js
        // can't infer the type from it and would hang. A buffer is unambiguous.
        const buf = await (await fetch(this.streamURL(item))).arrayBuffer();
        const book = window.ePub(buf);
        this._epubBook = book;
        this._epubRend = book.renderTo('epub-viewer', { width: '100%', height: '100%', flow: 'paginated' });
        // resume at the saved reading location (CFI) per item
        const loc = localStorage.getItem('gobby-epub-' + item.id);
        await this._epubRend.display(loc || undefined);
        this._epubRend.on('relocated', (l) => {
          if (l && l.start) localStorage.setItem('gobby-epub-' + item.id, l.start.cfi);
        });
        if (!this._epubFsHooked) {
          this._epubFsHooked = true;
          document.addEventListener('fullscreenchange', () => this._epubRend && this._epubRend.resize());
        }
      } catch (e) { /* unreadable epub */ }
      this.epubLoading = false;
    },
    epubPrev() { this._epubRend && this._epubRend.prev(); },
    epubNext() { this._epubRend && this._epubRend.next(); },
    epubFullscreen() {
      const el = this.$refs.epubReader;
      if (document.fullscreenElement) document.exitFullscreen();
      else if (el && el.requestFullscreen) el.requestFullscreen();
    },
    closeEpub() {
      if (this._epubBook) { try { this._epubBook.destroy(); } catch (e) {} }
      this._epubBook = null; this._epubRend = null;
    },

    // mkv/avi go through the server-side ffmpeg remux, whose piped fragmented mp4
    // can't seek by byte-range — so we drive playback + seek ourselves.
    needsRemux(item) { return ['mkv', 'avi'].includes(this.ext(item)); },
    videoOpen: false,
    videoRemux: false, // mkv/avi: seek via ?t= (fragmented mp4 = no native seek)
    videoDur: 0,       // total seconds (server for remux, element for native)
    seekVal: 0,        // current position in seconds
    playing: false,
    ctrlShow: true,    // auto-hiding controls
    audioTracks: [],   // [{idx,lang}] selectable audio (remux only)
    subTracks: [],     // [{idx,lang}] selectable subtitles (remux only)
    audioSel: 0,       // chosen audio idx
    subSel: -1,        // chosen subtitle idx, -1 = off
    subText: '',       // current on-screen subtitle line(s)
    subCfgOpen: false, // subtitle-appearance popover
    // subtitle look: persisted so it sticks across files/sessions.
    subCfg: { scale: 1, color: '#ffffff', boxed: false },
    subScales: [['S', 0.8], ['M', 1], ['L', 1.3], ['XL', 1.7]],
    subColors: ['#ffffff', '#ffe23d', '#8ce99a', '#74c0fc', '#ff8787'],
    loadSubCfg() {
      try {
        const s = JSON.parse(localStorage.getItem('gobby-subcfg') || '');
        if (s && typeof s === 'object') this.subCfg = { ...this.subCfg, ...s };
      } catch (e) {}
    },
    saveSubCfg() { localStorage.setItem('gobby-subcfg', JSON.stringify(this.subCfg)); },
    setSubScale(v) { this.subCfg.scale = v; this.saveSubCfg(); },
    setSubColor(c) { this.subCfg.color = c; this.saveSubCfg(); },
    toggleSubBox() { this.subCfg.boxed = !this.subCfg.boxed; this.saveSubCfg(); },
    // open the custom player. Native mp4/webm/mov seek the element directly; mkv/avi
    // are remuxed and seek by reloading the stream at ?t=.
    async playHere(item) {
      this.stopVideo();
      this.videoOpen = true;
      this.videoRemux = this.needsRemux(item);
      this.videoDur = 0;
      this.seekVal = 0;
      this._seekBase = 0;
      this.audioSel = 0;
      this.subSel = -1;
      this.subText = '';
      this._cues = [];
      this.audioTracks = [];
      this.subTracks = [];
      this._playItem = item;
      this._advancing = false;
      this.playerPoke();
      this.markOpened(item);
      // resume: saved position, unless in the intro or near the end
      let startAt = 0;
      const p = item.progress || 0, d = item.duration || 0;
      if (p > 30 && (!d || p < d * 0.9)) startAt = p;
      this.seekVal = startAt;
      this._loadVideo(startAt);
      const v = this.$refs.vplayer;
      if (startAt > 0 && !this.videoRemux) {
        v.addEventListener('loadedmetadata', () => { v.currentTime = startAt; }, { once: true });
      }
      if (!this.videoRemux) {
        v.onloadedmetadata = () => { this.videoDur = Math.floor(v.duration || 0); };
      }
      // Show the controls whenever fullscreen is entered/left (there's no mousemove
      // at that instant to bring the bar back).
      if (!this._fsHooked) {
        this._fsHooked = true;
        document.addEventListener('fullscreenchange', () => this.playerPoke());
      }
      // Track list (audio + subtitles) for every video, not just remux — mp4 can
      // carry multiple audio tracks and subtitles too.
      try {
        const r = await fetch(`/api/item/${item.id}/tracks`);
        const d = await r.json();
        if (this._playItem !== item) return; // user switched away
        if (this.videoRemux) this.videoDur = Math.floor(d.seconds || 0);
        this.audioTracks = d.audio || [];
        this.subTracks = d.subs || [];
      } catch (e) {}
    },
    // (re)point the <video> at the stream from `at` seconds with the current audio
    // track, then play. Used on open, seek, and audio change.
    _loadVideo(at) {
      const v = this.$refs.vplayer;
      const item = this._playItem;
      let url = this.streamURL(item);
      if (this.videoRemux) {
        const q = [];
        if (at > 0) q.push(`t=${at}`);
        if (this.audioSel > 0) q.push(`audio=${this.audioSel}`);
        if (q.length) url += '?' + q.join('&');
        this._seekBase = at;
      }
      // Drive position + subtitles from a steady timer instead of the element's
      // `timeupdate` event, which fires irregularly (and barely at all) over a
      // piped fragmented-mp4 stream — that's why subtitles never showed.
      clearInterval(this._tick);
      this._tick = setInterval(() => {
        if (v.paused && !v.currentTime) return;
        const t = (this.videoRemux ? this._seekBase : 0) + v.currentTime;
        this.seekVal = Math.floor(t);
        this._updateSub(t);
        if (!v.paused && this.seekVal - (this._savedAt || 0) >= 10) this._saveProgress(this.seekVal);
        // remux streams don't always fire @ended reliably; catch it here too
        if (this.videoRemux && v.ended && this.videoDur && t >= this.videoDur - 1.5) this.onEnded();
      }, 200);
      // Pin the current height before reloading so the box doesn't collapse to a
      // sliver while the new stream buffers (which made the whole player jump).
      if (v.clientHeight > 0) v.style.minHeight = v.clientHeight + 'px';
      v.src = url;
      v.load();
      const start = () => {
        v.play().catch(() => {});
        v.style.minHeight = ''; // release once the frame is back
        v.removeEventListener('canplay', start);
      };
      v.addEventListener('canplay', start);
    },
    // switch audio track: reload the remux from the current position with ?audio=N.
    setAudio(idx) {
      this.audioSel = idx;
      if (this.videoRemux) this._loadVideo(this.seekVal);
      this.playerPoke();
    },
    subURL(idx) { return `/api/item/${this._playItem?.id}/sub?track=${idx}`; },
    // Fetch the chosen subtitle, parse the VTT into cues, and paint them as an
    // overlay ourselves (native <track> doesn't render reliably over a piped
    // stream). -1 = off.
    async setSub(idx) {
      this.subSel = idx;
      this.subText = '';
      this._cues = [];
      this.playerPoke();
      if (idx < 0) return;
      // A newer selection must win even if an older fetch resolves later — guard with
      // a monotonically increasing token (that's the "changed several times and it
      // eventually worked" race).
      const token = (this._subReq = (this._subReq || 0) + 1);
      try {
        const r = await fetch(this.subURL(idx));
        const vtt = await r.text();
        if (token !== this._subReq) return; // superseded by a later setSub
        this._cues = this._parseVTT(vtt);
      } catch (e) {}
    },
    // minimal WebVTT parser → [{start, end, text}] in seconds. Handles MM:SS.mmm
    // and HH:MM:SS.mmm timestamps; strips inline tags.
    _parseVTT(vtt) {
      const cues = [];
      const toSec = (ts) => {
        const p = ts.trim().split(':');
        let s = parseFloat(p.pop());
        if (p.length) s += parseInt(p.pop()) * 60;
        if (p.length) s += parseInt(p.pop()) * 3600;
        return s;
      };
      for (const block of vtt.split(/\r?\n\r?\n/)) {
        const line = block.split(/\r?\n/).find(l => l.includes('-->'));
        if (!line) continue;
        const m = line.match(/([\d:.]+)\s*-->\s*([\d:.]+)/);
        if (!m) continue;
        const text = block.split(/\r?\n/).slice(block.split(/\r?\n/).findIndex(l => l.includes('-->')) + 1)
          .join('\n').replace(/<[^>]+>/g, '').trim();
        if (text) cues.push({ start: toSec(m[1]), end: toSec(m[2]), text });
      }
      return cues;
    },
    // show the cue at time t (wrapped in <span> so the boxed bg hugs the text)
    _updateSub(t) {
      if (!this._cues || !this._cues.length) { if (this.subText) this.subText = ''; return; }
      const c = this._cues.find(c => t >= c.start && t <= c.end);
      const html = c ? '<span>' + c.text.replace(/\n/g, '<br>') + '</span>' : '';
      if (html !== this.subText) this.subText = html;
    },
    langLabel(lang) {
      const m = { spa: 'Español', eng: 'English', fre: 'Français', fra: 'Français', ger: 'Deutsch', deu: 'Deutsch', ita: 'Italiano', por: 'Português', jpn: '日本語', chi: '中文', und: '—' };
      return m[lang] || (lang ? lang.toUpperCase() : '—');
    },
    // Label a track, numbering same-language ones ("Español", "Español 2") so a file
    // with two Spanish subtitle tracks (e.g. full + forced) is selectable, not two
    // identical entries where one looks broken.
    trackLabel(list, t) {
      const base = this.langLabel(t.lang);
      const same = list.filter(x => x.lang === t.lang);
      if (same.length < 2) return base;
      return `${base} ${same.indexOf(t) + 1}`;
    },
    togglePlay() {
      const v = this.$refs.vplayer;
      if (v.paused) v.play().catch(() => {}); else v.pause();
      this.playerPoke();
    },
    // playback finished → mark watched + auto-advance to the next album item
    async onEnded() {
      const item = this._playItem;
      if (!item || this._advancing) return;
      this._advancing = true;
      if (this.videoDur) this._saveProgress(this.videoDur);
      try {
        const r = await fetch(`/api/item/${item.id}/next`);
        const next = r.ok ? await r.json() : null;
        if (next && next.id) { this.playHere(next); return; }
      } catch (e) {}
      this._advancing = false;
      this.stopVideo();
    },
    // keyboard controls while the player is open: space play/pause, ←/→ seek, f fullscreen
    _onPlayerKey(e) {
      if (!this.videoOpen) return;
      const tag = (e.target.tagName || '').toLowerCase();
      if (tag === 'input' || tag === 'select' || tag === 'textarea') return;
      const rel = (d) => this.seekTo(Math.max(0, Math.min(this.videoDur || 1e9, this.seekVal + d)));
      switch (e.key) {
        case ' ': case 'k': e.preventDefault(); this.togglePlay(); break;
        case 'ArrowRight': e.preventDefault(); rel(10); break;
        case 'ArrowLeft': e.preventDefault(); rel(-10); break;
        case 'f': this.toggleFullscreen(); break;
        case 'Escape': if (!document.fullscreenElement) this.stopVideo(); break;
      }
    },
    // seek to an absolute second (shared by the bar click and keyboard)
    seekTo(t) {
      t = Math.floor(t);
      this.seekVal = t;
      if (this.videoRemux) this._loadVideo(t);
      else this.$refs.vplayer.currentTime = t;
      this.playerPoke();
    },
    // click on the seek bar → seek to that fraction.
    seekClick(e) {
      if (!this.videoDur) return;
      const box = this.$refs.seekbox.getBoundingClientRect();
      const frac = Math.min(1, Math.max(0, (e.clientX - box.left) / box.width));
      this.seekTo(frac * this.videoDur);
    },
    toggleFullscreen() {
      const el = this.$refs.vplayer.closest('.vplayer');
      if (document.fullscreenElement) document.exitFullscreen();
      else if (el.requestFullscreen) el.requestFullscreen();
      // entering/leaving fullscreen can leave the bar hidden with no mousemove to
      // bring it back — force it visible.
      this.playerPoke();
    },
    // show controls, then auto-hide after a few seconds of no movement while playing.
    playerPoke() {
      this.ctrlShow = true;
      clearTimeout(this._ctrlTimer);
      this._ctrlTimer = setTimeout(() => { if (this.playing) this.ctrlShow = false; }, 2600);
    },
    // stop and unload the player (leaving the detail, switching item, close button).
    stopVideo() {
      // exit fullscreen first, else the browser stays FS on the hidden player and freezes clicks
      if (document.fullscreenElement) { try { document.exitFullscreen(); } catch (e) {} }
      const v = this.$refs && this.$refs.vplayer;
      clearInterval(this._tick);
      // flush final position; near the end snap to duration so it reads as watched
      if (this._playItem && this.seekVal > 5) {
        const d = this.videoDur || this._playItem.duration || 0;
        this._saveProgress(d && this.seekVal >= d * 0.9 ? d : this.seekVal);
      }
      this._savedAt = 0;
      if (v) { v.pause(); v.onloadedmetadata = null; v.removeAttribute('src'); v.load(); }
      clearTimeout(this._ctrlTimer);
      this.videoOpen = false;
      this.videoRemux = false;
      this.videoDur = 0;
      this.seekVal = 0;
      this.playing = false;
      this.ctrlShow = true;
      this.audioTracks = [];
      this.subTracks = [];
      this.audioSel = 0;
      this.subSel = -1;
      this.subText = '';
      this._cues = [];
      this._playItem = null;
    },
    fmtTime(s) {
      s = Math.max(0, Math.floor(s));
      const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
      const p = (n) => String(n).padStart(2, '0');
      return h > 0 ? `${h}:${p(m)}:${p(sec)}` : `${m}:${p(sec)}`;
    },
    // record an explicit open (play/preview/open-with) so it shows in "continue".
    auraStyle: '',
    _auraFrom(a, b) {
      return `radial-gradient(120% 90% at 15% 10%, ${a}cc, transparent 62%),` +
             `radial-gradient(120% 95% at 85% 18%, ${b}cc, transparent 62%),` +
             `radial-gradient(110% 110% at 50% 100%, ${a}88, transparent 72%)`;
    },
    auraNeutral() {
      const dark = document.documentElement.getAttribute('data-theme') !== 'light';
      this.auraStyle = dark
        ? 'background-image:' + `radial-gradient(120% 90% at 20% 0%, #ffffff10, transparent 60%),radial-gradient(120% 90% at 80% 100%, #ffffff0a, transparent 60%)`
        : 'background-image:' + `radial-gradient(120% 90% at 20% 0%, #00000010, transparent 60%),radial-gradient(120% 90% at 80% 100%, #0000000a, transparent 60%)`;
    },
    auraFromCover(item) {
      if (!item || !(item.has_cover || item.cover_id)) { this.auraNeutral(); return; }
      this._auraFromImgURL(`/api/item/${item.cover_id || item.id}/cover`);
    },
    auraFromPoster(url) {
      if (!url) { this.auraNeutral(); return; }
      this._auraFromImgURL(url);
    },
    _auraFromImgURL(src) {
      const img = new Image();
      img.crossOrigin = 'anonymous';
      img.onload = () => {
        try {
          const c = document.createElement('canvas');
          const w = c.width = 32, h = c.height = 32;
          const ctx = c.getContext('2d');
          ctx.drawImage(img, 0, 0, w, h);
          const d = ctx.getImageData(0, 0, w, h).data;
          const px = [];
          for (let i = 0; i < d.length; i += 4) {
            const rr = d[i], gg = d[i + 1], bb = d[i + 2];
            const mx = Math.max(rr, gg, bb), mn = Math.min(rr, gg, bb);
            if (mx > 30 && mx < 250) px.push({ rr, gg, bb, sat: mx - mn });
          }
          if (px.length < 4) { this.auraNeutral(); return; }
          px.sort((a, b) => b.sat - a.sat);
          const vivid = px.slice(0, Math.max(6, px.length >> 2));
          const c1 = vivid[0];
          let c2 = vivid[0], best = -1;
          for (const p of vivid) {
            const dist = Math.abs(p.rr - c1.rr) + Math.abs(p.gg - c1.gg) + Math.abs(p.bb - c1.bb);
            if (dist > best) { best = dist; c2 = p; }
          }
          const hex = (p) => '#' + [p.rr, p.gg, p.bb].map(v => v.toString(16).padStart(2, '0')).join('');
          this.auraStyle = 'background-image:' + this._auraFrom(hex(c1), hex(c2));
        } catch (e) { this.auraNeutral(); }
      };
      img.onerror = () => this.auraNeutral();
      img.src = src;
    },
    async deleteFile(item) {
      if (!confirm(this.t('deleteConfirm').replace('{name}', item.title || item.rel_path))) return;
      const r = await fetch(`/api/item/${item.id}/file`, { method: 'DELETE' }).catch(() => null);
      if (r && r.ok) { this.back(); this.loadHome(); }
      else alert(this.t('deleteFailed'));
    },
    markOpened(item) { fetch(`/api/item/${item.id}/opened`, { method: 'POST' }).catch(() => {}); },
    // percent watched for the card bar; 0 hides it
    progressPct(it) {
      const p = it.progress || 0, d = it.duration || 0;
      if (!d || p <= 0) return 0;
      return Math.min(100, Math.round(p / d * 100));
    },
    _saveProgress(secs) {
      const item = this._playItem;
      if (!item) return;
      this._savedAt = secs;
      item.progress = secs;
      fetch(`/api/item/${item.id}/opened?t=${secs}`, { method: 'POST' }).catch(() => {});
    },
    streamURL(item) { return `/api/item/${item.id}/stream`; },
    absStream(item) { return (this.info.base || '') + `/api/item/${item.id}/stream`; },
    isAndroid() { return /android/i.test(navigator.userAgent); },
    // Android intent:// hands the stream to VLC / MX Player, which (unlike the
    // browser) decode AC3/DTS/EAC3. Passing the title labels it in the player;
    // browser_fallback_url reopens the stream in-browser if no app is installed.
    androidIntent(item) {
      const url = this.absStream(item);
      const type = item.kind === 'audio' ? 'audio/*' : 'video/*';
      const scheme = url.startsWith('https') ? 'https' : 'http';
      const noScheme = url.replace(/^https?:\/\//, '');
      const title = encodeURIComponent(item.title || '');
      const fallback = encodeURIComponent(url);
      return `intent://${noScheme}#Intent;action=android.intent.action.VIEW;type=${type};scheme=${scheme};S.title=${title};S.browser_fallback_url=${fallback};end`;
    },
    downloadURL(item) { return `/api/item/${item.id}/stream?dl=1`; },
    copied: false,
    async copyStream(item) {
      const text = this.absStream(item);
      let ok = false;
      // navigator.clipboard only works on HTTPS/localhost; over plain HTTP on the
      // LAN it silently fails, so fall back to a temp textarea + execCommand.
      try {
        if (navigator.clipboard && window.isSecureContext) {
          await navigator.clipboard.writeText(text);
          ok = true;
        }
      } catch (e) {}
      if (!ok) {
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        try { ok = document.execCommand('copy'); } catch (e) {}
        document.body.removeChild(ta);
      }
      if (ok) { this.copied = true; setTimeout(() => this.copied = false, 2000); }
    },

    coverBusy: false,
    // upload a custom cover image for any item (books, music, files, anything)
    async uploadCover(item, ev) {
      const f = ev.target.files[0];
      ev.target.value = '';
      if (!f || !item) return;
      this.coverBusy = true;
      try {
        const r = await fetch(`/api/item/${item.id}/cover`, { method: 'POST', body: f });
        if (!r.ok) { alert(await r.text()); return; }
        item.has_cover = true;
        item.cover_id = item.id;      // its own cover now
        item._bust = Date.now();      // cache-bust the <img>
      } finally { this.coverBusy = false; }
    },
    // the item in a series/album that carries (or should carry) the cover
    groupCover() {
      const g = this.top.group;
      if (!g || !g.items || !g.items.length) return null;
      return g.items.find(i => i.has_cover) || g.items[0];
    },
    groupID: '',
    // re-identify a whole series/album by external id (applies to the cover-carrier)
    async refetchGroup() {
      const rep = this.groupCover();
      if (!rep) return;
      rep.imdb_id = this.groupID;
      await this.refetch(rep);
      this.groupID = '';
      // Meta now propagated to every episode in the DB — reload the show so the
      // synopsis/cast appear without leaving and re-opening the series page.
      const g = this.top.group;
      if (g && g.items && g.items[0]) {
        const r = await fetch(`/api/item/${g.items[0].id}`);
        if (r.ok) g.show = await r.json();
      }
    },

    refetching: false,
    async refetch(item) {
      this.refetching = true;
      const r = await fetch(`/api/item/${item.id}/refetch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ imdb_id: item.imdb_id || '' }),
      });
      if (r.ok) {
        const d = await r.json();
        Object.assign(item, d.item);
        item._bust = Date.now(); // cache-bust the cover img
      }
      this.refetching = false;
    },

    artStyle(item) {
      if (!item || !item.has_cover) return 'display:none';
      // Home rows point at the sibling that actually holds the cover (series only
      // tag one episode); fall back to the item's own id everywhere else.
      const cid = item.cover_id || item.id;
      const bust = item._bust ? `?b=${item._bust}` : '';
      return `background-image:url(/api/item/${cid}/cover${bust})`;
    },
    // Episodes reuse the series poster (only the first episode carries a cover),
    // so every episode thumb shows art without downloading N copies.
    epStyle(ep, group) {
      if (ep && ep.has_cover) return this.artStyle(ep);
      const poster = group && group.items && group.items.find(i => i.has_cover);
      return poster ? this.artStyle(poster) : 'display:none';
    },

    // ---- metadata chips (technical, from the local filename parse) ----
    tech(it) { return (it && it.meta && it.meta.tech) || null; },
    techChips(it) {
      const t = this.tech(it);
      if (!t) return [];
      const c = [];
      if (t.resolution) c.push({ ic: 'monitor', v: t.resolution });
      if (t.video_codec) c.push({ ic: 'film', v: t.video_codec });
      if (t.source) c.push({ ic: 'download', v: t.source });
      if (t.audio_codec) c.push({ ic: 'volume', v: t.audio_codec + (t.channels ? ' ' + t.channels : ''), warn: this.badAudio(t.audio_codec) });
      if (t.languages && t.languages.length) c.push({ ic: 'globe', v: t.languages.join(', ') });
      return c;
    },
    // AC3/DTS/EAC3/TrueHD can't be decoded by browsers → silent video.
    // Surfaced as a quiet warn-tinted chip (see techChips), not a banner.
    badAudio(codec) {
      if (!codec) return false;
      return ['AC3', 'EAC3', 'DTS', 'DTSHD', 'TRUEHD', 'ATMOS']
        .includes(codec.toUpperCase().replace(/[-\s]/g, ''));
    },
    metaSource(it) {
      const s = it && it.meta && it.meta.source;
      return { cinemeta: 'Cinemeta', openlibrary: 'Open Library', itunes: 'iTunes', musicbrainz: 'MusicBrainz' }[s] || '';
    },

    // ---- background progress: scan + enrich, one unified bar ----
    async enrich(force = false) {
      if (this.busy) return;
      const q = force ? '&force=1' : '';
      // Home isn't a content kind — enrich everything. Otherwise scope to the tab.
      const kind = (this.tab === 'home' || this.tab === 'files' || this.tab === 'watch') ? 'all' : this.tab;
      const r = await fetch(`/api/enrich?kind=${kind}${q}`, { method: 'POST' });
      if (!r.ok) return;
      this.busy = true;
      this.prog = { done: 0, total: 0, found: 0, phase: 'enrich' };
      this.pollProgress();
    },
    async stopEnrich() {
      await fetch('/api/enrich/stop', { method: 'POST' });
    },
    // Poll scan first (it runs before enrich); whichever is running drives the
    // bar. Keeps polling while either is active, then does a final refresh.
    async pollProgress() {
      if (this.pollTimer) clearTimeout(this.pollTimer);
      let p = await (await fetch('/api/scan/status')).json();
      if (!p.running) p = await (await fetch('/api/enrich/status')).json();
      this.prog = { done: p.done, total: p.total, found: p.found, phase: p.phase };
      if (p.running) {
        this.busy = true;
        this.loadInfo(); // new sections may appear as the scan finds media
        this.load();
        this.pollTimer = setTimeout(() => this.pollProgress(), 800);
      } else if (this.busy) {
        this.busy = false;
        this.success();
        this.loadInfo();
        this.load();
      }
    },
    progPct() {
      return this.prog.total ? Math.round((this.prog.done / this.prog.total) * 100) : 0;
    },
    // scan doesn't know the file count up front → indeterminate bar.
    indeterminate() {
      return this.busy && !this.prog.total;
    },
    progLabel() {
      const phase = this.prog.phase === 'scan' ? this.t('scanning') : this.t('fetching');
      if (this.indeterminate()) return `${phase} · ${this.prog.done}`;
      return `${phase} · ${this.prog.done}/${this.prog.total} · ${this.progPct()}%`;
    },

    async loadWatch() {
      const r = await fetch('/api/watchlist');
      this.watch = (await r.json()) || [];
    },
    // online title search for the watchlist picker (Cinemeta + Open Library)
    wq: '',
    wresults: [],
    wsearching: false,
    // active type chips: filter the saved list AND scope the online search. None = all.
    searchKinds: [],
    newWatchNote: '',
    // which watchlist types have an online provider (the rest are manual-only)
    onlineKinds: ['movie', 'series', 'book', 'audio', 'game'],
    allWatchKinds: ['movie', 'series', 'audio', 'book', 'game', 'other'],
    toggleKind(k) {
      const i = this.searchKinds.indexOf(k);
      if (i >= 0) this.searchKinds.splice(i, 1);
      else this.searchKinds.push(k);
      this.searchWatch();
    },
    kindOn(k) { return this.searchKinds.includes(k); },
    // the type chips double as a filter of the saved list: none active = show all
    filteredWatch() {
      if (!this.searchKinds.length) return this.watch;
      return this.watch.filter(w => this.searchKinds.includes(w.kind || 'other'));
    },
    async searchWatch() {
      const q = this.wq.trim();
      // no chips active = search every online provider; else only the active ones
      const active = this.searchKinds.length ? this.searchKinds : this.onlineKinds;
      const kinds = active.filter(k => this.onlineKinds.includes(k));
      if (!q || !kinds.length) { this.wresults = []; return; }
      this.wsearching = true;
      try {
        const r = await fetch('/api/search?q=' + encodeURIComponent(q) + '&kinds=' + kinds.join(','));
        this.wresults = r.ok ? (await r.json()) || [] : [];
      } finally { this.wsearching = false; }
    },
    async pickWatch(hit) {
      await fetch('/api/watchlist', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: hit.title, kind: hit.kind, poster: hit.poster, year: hit.year }),
      });
      this.wq = ''; this.wresults = [];
      this.loadWatch();
    },
    // the type a manual add uses: the first active chip, else "other".
    manualKind() { return this.searchKinds[0] || 'other'; },
    // manual add for anything the online search can't find (games, notes):
    // the typed query becomes the title, typed as the active chip.
    async addManual() {
      const t = this.wq.trim();
      if (!t) return;
      await fetch('/api/watchlist', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: t, kind: this.manualKind(), note: this.newWatchNote }),
      });
      this.wq = ''; this.wresults = []; this.newWatchNote = '';
      this.loadWatch();
    },
    async toggle(w) {
      await fetch(`/api/watchlist/${w.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ done: !w.done }),
      });
      this.loadWatch();
    },
    async delWatch(w) {
      await fetch(`/api/watchlist/${w.id}`, { method: 'DELETE' });
      this.loadWatch();
    },
    // open a watchlist entry's detail page (same layout as media)
    openWatch(w) { this.auraFromPoster(w.poster); this.pushView({ view: 'watchitem', watch: { ...w } }); },
    async saveWatchNote(w) {
      await fetch(`/api/watchlist/${w.id}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: w.note || '' }),
      });
      this.loadWatch();
    },
    // ---- watchlist cover (URL or file) + custom fields ----
    async saveWatchField(w, key, val) {
      await fetch(`/api/watchlist/${w.id}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ [key]: val }),
      });
      this.loadWatch();
    },
    setWatchPosterURL(w) {
      const url = prompt(this.t('coverUrlPrompt'));
      if (url === null) return;
      w.poster = url.trim();
      this.saveWatchField(w, 'poster', w.poster);
    },
    async uploadWatchCover(w, ev) {
      const f = ev.target.files && ev.target.files[0];
      if (!f) return;
      const dataURI = await this.fileToDataURI(f);
      w.poster = dataURI;
      this.saveWatchField(w, 'poster', dataURI);
    },
    fileToDataURI(file) {
      return new Promise((res, rej) => {
        const r = new FileReader();
        r.onload = () => res(r.result); r.onerror = rej;
        r.readAsDataURL(file);
      });
    },
    addWatchField(w) {
      if (!w.fields) w.fields = [];
      w.fields.push({ k: '', v: '' });
    },
    removeWatchField(w, i) {
      w.fields.splice(i, 1);
      this.saveWatchField(w, 'fields', w.fields);
    },
    saveWatchFields(w) { this.saveWatchField(w, 'fields', w.fields || []); },
    // Set a media item's cover from a URL: fetch it here, upload the bytes to the
    // existing cover endpoint (which only takes a file), so URL works everywhere.
    async setCoverURL(item) {
      const url = prompt(this.t('coverUrlPrompt'));
      if (!url) return;
      this.coverBusy = true;
      try {
        const img = await (await fetch(url.trim())).blob();
        const r = await fetch(`/api/item/${item.id}/cover`, { method: 'POST', body: img });
        if (r.ok) { item.has_cover = true; item._bust = Date.now(); }
      } catch (e) { /* bad URL / CORS — silently ignore */ } finally { this.coverBusy = false; }
    },
    watchIcon(kind) { return this.svg({ movie: 'clapper', series: 'film', book: 'book', audio: 'music', game: 'gamepad' }[kind] || 'bookmark'); },
  };
}
