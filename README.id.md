<div align="center">

# jsmap-pro

**Temukan JavaScript. Pulihkan source map. Petakan pola sensitif.**

CLI Go untuk memindai aset frontend secara streaming, merekonstruksi source map yang terbuka, dan menormalkan string JavaScript statis.

[![CI](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml/badge.svg)](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Hzkun001/jsmap-pro)](https://github.com/Hzkun001/jsmap-pro/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](go.mod)

[English](README.md) · **Bahasa Indonesia**

</div>

## Mulai Cepat

Nama perintahnya **`jsmap`**. Nama repository tetap `jsmap-pro`.

### Pasang sekali dari source — Linux / macOS

Memerlukan **Go 1.24+** dan Git. Jika repo sudah di-clone, buka direktorinya lalu lewati dua baris pertama:

```sh
git clone https://github.com/Hzkun001/jsmap-pro.git
cd jsmap-pro
mkdir -p "$HOME/.local/bin"
go build -ldflags="-s -w" -o "$HOME/.local/bin/jsmap" .
export PATH="$HOME/.local/bin:$PATH"
```

### Jalankan

```sh
jsmap example.com
```

Ganti `example.com` dengan website Anda yang memuat aset JavaScript. HTTPS dipilih otomatis. Untuk HTTP, tulis `http://` secara eksplisit. Laporan tersimpan di `jsmap-output/`.

```sh
jsmap example.com -o hasil
jsmap -har capture.har -o hasil-har
jsmap -d ./assets -o hasil-lokal
jsmap -h
```

Opsi boleh sebelum atau sesudah target. Folder output harus baru atau kosong.

> [!TIP]
> Agar `jsmap` tetap tersedia di terminal baru, tambahkan `export PATH="$HOME/.local/bin:$PATH"` ke `~/.bashrc` atau `~/.zshrc` sekali saja.

### Tanpa Go — binary release

Unduh lalu ekstrak arsip sesuai OS/CPU dari [Releases](https://github.com/Hzkun001/jsmap-pro/releases/latest). Mulai **v0.1.4**, arsip berisi `jsmap` (Windows: `jsmap.exe`); rilis sebelumnya berisi `jsmap-pro`.

Dari direktori hasil ekstraksi di Linux/macOS:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 jsmap "$HOME/.local/bin/jsmap"
export PATH="$HOME/.local/bin:$PATH"
jsmap example.com
```

Di Windows, jalankan `.\jsmap.exe example.com` melalui PowerShell dari direktori hasil ekstraksi, atau tambahkan direktorinya ke PATH untuk memakai `jsmap` langsung.

### Jalankan langsung dari salinan repo

```sh
go run . example.com
```

Untuk membuat binary di direktori saat ini:

```sh
go build -o jsmap .
./jsmap example.com
```

## Mode input

Pilih satu sumber input untuk setiap pemindaian:

| Mode | Contoh | Kegunaan |
|:--|:--|:--|
| Website | `-u https://example.com` | Temukan aset dari tag `<script>` dan referensi `.js` statis di inline script. |
| Daftar URL | `-l urls.txt` | Pindai URL JavaScript yang sudah dikumpulkan. |
| HAR offline | `-har capture.har` | Analisis respons JavaScript dari HTTP Archive tanpa menghubungi server. |
| Direktori lokal | `-d ./assets` | Analisis file `.js` di disk, termasuk subdirektori. |

```sh
jsmap -l urls.txt -c 20 -o hasil-url
jsmap -har capture.har -o hasil-har
jsmap -d ./assets -o hasil-lokal
```

`urls.txt` berisi satu URL HTTP(S) per baris. Baris kosong dan komentar yang diawali `#` diabaikan. Direktori output harus baru atau kosong agar hasil sebelumnya tetap tersimpan.

## Pola yang dipindai

| Kategori | Pola utama |
|:--|:--|
| WebCrypto | `crypto.subtle.encrypt/decrypt/sign/verify/importKey` |
| Crypto pihak ketiga | `CryptoJS`, `forge.cipher`, `JSEncrypt`, `U2FsdGVkX1` |
| Header signature | `X-Signature`, `X-Sign`, `X-Timestamp`, `X-Request-Id` |
| Base64 | `atob()`, `btoa()` |

Setiap temuan mencatat file, nomor baris, kategori, dan cuplikan kode. JavaScript mentah dipindai sambil dialirkan ke disk. Jika pola getter array string yang didukung ditemukan, inliner AST juga memindai hasil normalisasinya di `analyzed/`.

## Source map

Jika JavaScript diakhiri `//# sourceMappingURL=...`, jsmap memeriksa map dan merekonstruksi `sourcesContent` ke `unpacked/`. Status **Fully Reconstructed via SourceMap** hanya dipakai jika semua sumber berhasil ditulis. Konten sumber yang tidak tersedia dan path yang tidak aman akan dicatat.

Mode HAR dan direktori berjalan offline: source map hanya dibaca jika tersedia di HAR atau direktori input.

## Opsi CLI

| Opsi | Default | Keterangan |
|:--|:--|:--|
| `-u` | — | URL website |
| `-l` | — | File berisi URL JavaScript |
| `-har` | — | HTTP Archive untuk analisis offline |
| `-d` | — | Direktori JavaScript lokal |
| `-c` | `10` | Jumlah worker HTTP |
| `-o` | `jsmap-output` | Direktori output baru atau kosong |
| `-timeout` | `30s` | Timeout untuk setiap request HTTP |

## Build dan kontribusi

Memerlukan Go **1.24 atau lebih baru**. Batas minimum ini diperlukan oleh `os.Root` untuk menjaga ekstraksi source map tetap di dalam direktori output.

```sh
go build -ldflags="-s -w" -o jsmap .
go test -race ./...
go vet ./...
```

Kontribusi, laporan bug, dan permintaan fitur dipersilakan. Lihat [CONTRIBUTING.md](CONTRIBUTING.md), [Kode Etik](CODE_OF_CONDUCT.md), dan [Kebijakan Keamanan](SECURITY.md).

## Cakupan dan batasan

- Crawling website bersifat statis dan terbatas pada halaman utama; tool tidak menjalankan browser atau JavaScript.
- Path chunk yang dibentuk saat runtime, source map inline `data:`, dan indexed map `sections` belum didukung.
- Inlining terbatas pada array string statis di awal file dan getter sederhana di top-level. Hasilnya membantu pemindaian, tetapi tidak membuktikan perilaku runtime.
- Batas respons: HTML 16 MiB, JavaScript 512 MiB, source map 64 MiB, dan input AST 25 MiB. Baris minified panjang tetap dipindai bertahap.
- Jeda request acak 150–300 ms dan header browser mengurangi laju request, tetapi tidak menjamin server menerima pemindaian.

## Lisensi

MIT. Lihat [LICENSE](LICENSE).
