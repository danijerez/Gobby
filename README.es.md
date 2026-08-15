<div align="center">
  <img src=".github/logo.svg" alt="Gobby" width="120">

  <h1>Gobby</h1>

  <p><em>Gobby es un elfo libre. Gobby sirve tu multimedia, y solo te la sirve a ti.</em></p>

  <p>
    <img alt="Go" src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white">
    <img alt="SQLite" src="https://img.shields.io/badge/SQLite-003B57?logo=sqlite&logoColor=white">
    <img alt="Alpine.js" src="https://img.shields.io/badge/Alpine.js-8BC0D0?logo=alpinedotjs&logoColor=black">
    <img alt="MCP" src="https://img.shields.io/badge/MCP-server-6E56CF">
    <img alt="Cloudflare" src="https://img.shields.io/badge/Cloudflare-Tunnel-F38020?logo=cloudflare&logoColor=white">
    <img alt="version" src="https://img.shields.io/badge/version-0.2.1-2ea44f">
    <img alt="license" src="https://img.shields.io/badge/license-MIT-blue">
  </p>

  <p><a href="README.md">English</a> · <strong>Español</strong></p>
</div>

---

¡Hola! Gobby está aquí para ayudar, oh sí.

Gobby es un pequeño binario de Go — sin amos que instalar, sin bases de datos que
alimentar. Apuntas a Gobby a una carpeta y Gobby escanea tus películas, series,
música, libros y ficheros sueltos, los recuerda en un ordenado cajón SQLite, y
abre dos puertas en **el mismo puerto**:

- una **página web** para tu móvil o navegador (Gobby imprime un enlace y un QR en la terminal), y
- un **servidor MCP** 🧙‍♂️ para que una **inteligencia suprema** pueda buscar en tu biblioteca.

Gobby nunca *posee* nada. Gobby solo **explora** lo que ya tienes, **descarga** las
pequeñas carátulas y detalles de servicios gratuitos, y — si se lo pides con
amabilidad — **abre un túnel** para que puedas ver desde lejos. En el cajón de
Gobby no vive ningún derecho de autor, oh no. Gobby es un elfo libre, no un pirata.

Tus pequeñas ediciones — títulos, notas, valoraciones, la lista de pendientes —
viven **solo en el cajón**. Gobby se plancharía las manos antes de tocar tus
ficheros originales.

> Hecho con **vibe coding** ✨ — Gobby se escribió de la mano de una **inteligencia
> suprema**, y luego se revisó línea por línea. Un elfo libre y un mago listo, oh sí. 🧦🪄

## Conseguir Gobby 📥

Coge el binario para tu plataforma de la página de [Releases](https://github.com/danijerez/Gobby/releases)
y déjalo en la carpeta donde quieras que viva. Eso es todo — un fichero, sin instalación.
(Mira el [CHANGELOG](CHANGELOG.md) para ver las novedades.)

Para reproducir `.mkv`/`.avi` en el navegador Gobby necesita un **ffmpeg** slim. La
primera vez que hace falta, Gobby lo descarga automáticamente de su propia release
a la carpeta del binario (igual que hace con cloudflared) — nada que instalar. Si
prefieres poner el tuyo, deja un `ffmpeg` junto al binario de Gobby o ten uno en el
`PATH` y Gobby usará ese. Los demás formatos se descargan y se transmiten a la TV
igual sin ffmpeg — solo que no se reproducen dentro del navegador.

## Compilar 🔨

Necesita **Go 1.26+**. SQLite en Go puro (modernc) significa sin CGO y sin toolchain de C.

```sh
go build -o gobby ./src         # un binario para tu plataforma actual
./build/publish.sh              # compila cruzado Windows + Linux + macOS en bin/
./build/publish.sh v0.2.0       # ...marcando una versión explícita
```

El **ffmpeg** slim se compila aparte (solo si quieres regenerarlo — las releases ya
traen uno). Necesita Docker y produce un binario estático de ~20 MB con todos los
demuxers/decoders pero solo los encoders mp4/aac que Gobby usa de verdad:

```sh
./build/ffmpeg-slim/build.sh    # deja ffmpeg.exe junto al script
```

## Cómo llamar a Gobby 📣

```sh
gobby                              # escanea junto al binario (o una carpeta arriba si está vacío)
gobby -p D:\                       # escanea esta carpeta
gobby -p E:\ -library mi-disco     # mantiene la misma biblioteca aunque cambie la letra de unidad
gobby -port 9000                   # otra puerta (8420 por defecto)
gobby -h                           # Gobby se explica a sí mismo
```

Gobby imprime su nombre, versión y un enlace (con un QR) cuando despierta.

Luego abre el enlace que Gobby imprime, o escanea el QR desde tu móvil. Misma casa, misma puerta.

## Gobby en una caja 🐳

Gobby corre igual de feliz sin pantalla dentro de un contenedor. El repo trae un
`Dockerfile` y un `docker-compose.yml`. Apunta el volumen `media` a tu biblioteca
y:

```sh
docker compose up
```

Dos volúmenes, cada uno con su función:

- **`/media`** — tu biblioteca, montada **solo-lectura** (`:ro`). Gobby solo la lee.
- **`/data`** — donde vive `gobby.db`, un volumen **persistente** para que tu
  catálogo sobreviva a los reinicios.

ffmpeg viene dentro de la imagen, así que los mkv/avi se reproducen sin descargar
nada. Configura con variables de entorno (todas opcionales): `GOBBY_PATH` (carpeta
a escanear, por defecto `/media`), `GOBBY_DATA` (carpeta de la db, por defecto
`/data`), `GOBBY_PORT`, `GOBBY_LIBRARY`.

Ejecutar el binario suelto sigue funcionando exactamente igual que antes — el
contenedor es solo otra forma de despertar a Gobby, no un reemplazo.

## Qué sabe hacer Gobby 🎬

- **Escanea sin conexión** — lee nombres de fichero y etiquetas incrustadas, no necesita internet para catalogar.
- **Descarga carátulas y detalles** — de servicios gratuitos y sin claves (ver abajo) cuando se lo pides.
- **Series, álbumes, autores** — agrupados con orden, con temporadas, episodios y sinopsis.
- **Pestaña Ficheros** — un árbol de tamaños en color de todo el disco, para ver qué se come el espacio.
- **Pendientes** — apunta cualquier cosa para ver/leer/escuchar después, con campos personalizados y carátulas.
- **Reproduce en el navegador** — los mkv/avi se remultiplexan al vuelo con un
  **ffmpeg** slim (vídeo copiado, audio a AAC), así se reproducen donde los
  navegadores normalmente se niegan.
- **Elige el audio, pon subtítulos** — escoge una pista de audio y carga subtítulos
  incrustados como WebVTT (con tamaño, color y fondo ajustables), todo descodificado
  por el mismo pequeño ffmpeg.
- **Retoma donde lo dejaste** — recuerda la posición de reproducción, con una barra de
  progreso en el estante; al acabar un episodio **reproduce solo el siguiente** (música también).
- **Reproductor con teclado** — espacio para pausar, ←/→ para saltar, `f` para pantalla completa.
- **Lee epub en línea** — un lector paginado (epub.js) que recuerda tu página.
- **Búsqueda por voz** — toca el micro y dicta tu búsqueda (donde el navegador lo soporte).
- **Abrir, subir y borrar** (en el equipo que ejecuta Gobby) — abre la carpeta de un
  fichero en tu explorador, suelta un fichero en una carpeta desde la pestaña Ficheros,
  o borra un fichero del disco (solo-host, con confirmación). El resto queda en solo-lectura.
- **Filtra el árbol de Ficheros** — un buscador que reduce el árbol de tamaños al escribir.
- **Transmite a la TV** — Gobby también sirve por **HTTPS** (autofirmado, en el puerto siguiente)
  para que el Cast del navegador funcione en la red local; el QR apunta ahí.
- **Acceso remoto** — un clic abre un **túnel Cloudflare** temporal (enlace público + QR),
  protegido por una clave por túnel: una URL suelta por sí sola no llega a tu biblioteca.
- **Previsualiza PDF/audio/imágenes en línea**, y un sonidito para cada toque.
- **Solo lectura para invitados** — cualquiera que llegue por el túnel público puede mirar, no editar.
- **Se actualiza solo** — desde la pantalla *Acerca de*, Gobby comprueba GitHub por
  una release más nueva y sustituye su propio binario (y ffmpeg) en el sitio.
  Reinicia y ya está nuevo.

## Los servicios gratuitos que Gobby toma prestados (nunca claves) 🎁

| Para | Servicio |
| --- | --- |
| Películas y series | Cinemeta |
| Libros | Open Library |
| Música | MusicBrainz + Cover Art Archive |
| Juegos | CheapShark |
| Carátulas de reserva | iTunes Search |
| Acceso remoto | Cloudflare Tunnel |
| Transmitir a TV | Google Cast |

## De qué está hecho Gobby 🧱

Go · SQLite (modernc) · Alpine.js · [cuelume](https://github.com/Danilaa1/cuelume) (sonido) · MCP Go SDK · dhowden/tag · rsc.io/qr · un [ffmpeg](https://ffmpeg.org) slim (reproducción)

## MCP — para los magos 🧙

Gobby expone un servidor MCP en `/mcp` en el mismo puerto. Añádelo y tu **inteligencia suprema** 🧙‍♂️ podrá:

- **Explorar y buscar** — `search_media`, `get_media`, `browse_library`, `recent_media`, `library_info`
- **Reproducir** — `play_media` (abre en el equipo servidor *y* devuelve un enlace web para verlo en cualquier dispositivo)
- **Editar** — `update_media` (título/notas/valoración/…), `set_media_section`, `set_media_progress`, `enrich_media`
- **Watchlist** — `search_titles`, `add_to_watchlist`, `list_watchlist`, `remove_from_watchlist`, `set_watchlist_done`

```sh
claude mcp add --transport http gobby http://localhost:8420/mcp
```

## Licencia y créditos 📜

El código propio de Gobby es **MIT** — ver [LICENSE](LICENSE). Las bibliotecas
incluidas y el binario de ffmpeg mantienen sus propias licencias:

| Componente | Licencia | Notas |
| --- | --- | --- |
| [ffmpeg](https://ffmpeg.org) (build slim) | **LGPL-2.1-or-later** | Compilado **sin** `--enable-gpl` / `--enable-nonfree`. Fuente: ffmpeg.org; configuración exacta del build en [build/ffmpeg-slim/](build/ffmpeg-slim/). Se distribuye como un binario separado y sin modificar que puedes reemplazar. |
| Go std + `golang.org/x/*` | BSD-3-Clause | |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (+ libc, memory, …) | BSD-3-Clause | SQLite en Go puro |
| [dhowden/tag](https://github.com/dhowden/tag) | BSD-2-Clause | metadatos de audio |
| [mdp/qrterminal](https://github.com/mdp/qrterminal), [rsc.io/qr](https://pkg.go.dev/rsc.io/qr) | MIT / BSD-3 | QR de terminal + PNG |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), google/jsonschema-go, google/uuid | Apache-2.0 / BSD-3 | |
| [Alpine.js](https://alpinejs.dev) | MIT | incluido en `src/web/` |
| [cuelume](https://github.com/Danilaa1/cuelume) | MIT | sonidos de toque, incluido en `src/web/` |
| [epub.js](https://github.com/futurepress/epub.js) | BSD-2-Clause | lector epub en línea, incluido en `src/web/` |
| [JSZip](https://stuk.github.io/jszip/) | MIT | usado bajo MIT (JSZip es MIT-o-GPLv3); incluido en `src/web/` |

**Logo:** el goblin es [«Goblin» de Caro Asercion](https://game-icons.net/1x1/caro-asercion/goblin.html),
con licencia [CC BY 3.0](https://creativecommons.org/licenses/by/3.0/deed.es), recoloreado y
puesto sobre un fondo para Gobby.

**Sobre el contenido que Gobby indexa:** Gobby **no** distribuye contenido ni
material con derechos de autor. Solo lee los ficheros que ya tienes en tu propio
disco y descarga carátulas y metadatos de servicios públicos y sin claves —
[Cinemeta](https://www.stremio.com/), [Open Library](https://openlibrary.org),
[MusicBrainz](https://musicbrainz.org) / [Cover Art Archive](https://coverartarchive.org),
[CheapShark](https://www.cheapshark.com) y la [iTunes Search API](https://performance-partners.apple.com/search-api) —
usados dentro de sus términos, para catalogar de forma personal y de solo lectura.
El túnel Cloudflare y Google Cast son comodidades opcionales que inicia el usuario.
Lo que hagas con tu propia biblioteca es, como siempre, cosa tuya — Gobby es un
elfo libre, no tu abogado. 🧦

---

<div align="center"><sub>Gobby no tiene amo. Gobby tiene un amigo. 🧦</sub></div>
