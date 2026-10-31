<div align="center">

# jsmap-pro

**Temukan JavaScript. Buka source map. Petakan pola sensitif.**

CLI Go untuk memindai aset frontend secara streaming, merekonstruksi source map, dan menormalkan string JavaScript statis.

[![CI](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml/badge.svg)](https://github.com/Hzkun001/jsmap-pro/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Hzkun001/jsmap-pro)](https://github.com/Hzkun001/jsmap-pro/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](go.mod)

</div>

## Quick Start

Sudah punya Go? Jalankan pemindaian pertama dengan satu perintah:

```sh
go run ./cmd/jsmap -u https://example.com -o hasil
```

> [!TIP]
> Punya binary? Cukup jalankan `./jsmap-pro -u https://example.com -o hasil`.

Build binary mandiri:

```sh
go build -ldflags="-s -w" -o jsmap-pro ./cmd/jsmap
```

Hasil tersimpan di `hasil/`: file JavaScript mentah ada di `raw/`, source map yang terbuka direkonstruksi di `unpacked/`, dan ringkasan ada di `summary.json` serta `summary.txt`.

## Mode input

Pilih satu sumber input untuk setiap pemindaian:

| Mode | Contoh | Kapan digunakan |
|:--|:--|:--|
| Website | `-u https://example.com` | Temukan aset dari tag `<script>` dan referensi `.js` statis di inline script. |
| Daftar URL | `-l urls.txt` | Pindai URL JavaScript yang sudah dikumpulkan sebelumnya. |
| HAR offline | `-har capture.har` | Analisis respons JavaScript dari HTTP Archive tanpa menghubungi server. |
| Direktori lokal | `-d ./assets` | Analisis file `.js` di disk, termasuk subdirektori. |

```sh
./jsmap-pro -l urls.txt -c 20 -o hasil-url
./jsmap-pro -har capture.har -o hasil-har
./jsmap-pro -d ./assets -o hasil-lokal
```

`urls.txt` berisi satu URL HTTP(S) per baris. Baris kosong dan komentar `#` dilewati. Output harus berupa direktori baru atau direktori kosong supaya hasil sebelumnya tidak tertimpa.

## Yang dianalisis

| Kategori | Pola utama |
|:--|:--|
| WebCrypto | `crypto.subtle.encrypt/decrypt/sign/verify/importKey` |
| Crypto pihak ketiga | `CryptoJS`, `forge.cipher`, `JSEncrypt`, `U2FsdGVkX1` |
| Header signature | `X-Signature`, `X-Sign`, `X-Timestamp`, `X-Request-Id` |
| Base64 | `atob()`, `btoa()` |

Setiap temuan mencatat file, nomor baris, kategori, dan cuplikan. File mentah dianalisis sambil dialirkan ke disk; inliner AST juga memindai hasil normalisasi di `analyzed/` bila menemukan pola getter array string yang aman dikenali.

## Source map

Saat JavaScript berakhir dengan `//# sourceMappingURL=...`, jsmap-pro memeriksa map dan merekonstruksi `sourcesContent` ke `unpacked/`. Status **Fully Reconstructed via SourceMap** hanya diberikan jika seluruh sumber berhasil ditulis. Sumber yang tidak disertakan atau path yang tidak aman ditandai di laporan.

Mode HAR dan direktori bekerja offline: source map hanya dimuat jika tersedia di HAR atau di dalam direktori input.

## Opsi CLI

| Opsi | Default | Keterangan |
|:--|:--|:--|
| `-u` | — | URL halaman website |
| `-l` | — | File daftar URL JavaScript |
| `-har` | — | File HTTP Archive untuk analisis offline |
| `-d` | — | Direktori JavaScript lokal |
| `-c` | `10` | Jumlah worker HTTP |
| `-o` | `jsmap-output` | Direktori hasil yang baru atau kosong |
| `-timeout` | `30s` | Timeout tiap request HTTP |

## Build dan kontribusi

Memerlukan Go **1.24 atau lebih baru**. Batas ini diperlukan oleh API `os.Root` yang menjaga ekstraksi source map tetap di dalam direktori output.

```sh
go build -ldflags="-s -w" -o jsmap-pro ./cmd/jsmap
go test -race ./...
go vet ./...
```

Kontribusi, laporan bug, dan saran fitur dipersilakan. Baca [CONTRIBUTING.md](CONTRIBUTING.md), [Code of Conduct](CODE_OF_CONDUCT.md), dan [Security Policy](SECURITY.md).

## Batasan

- Crawling website bersifat statis dan hanya membaca halaman utama; tool tidak menjalankan browser atau JavaScript.
- Pemetaan chunk yang dibentuk saat runtime, source map inline `data:`, dan indexed map `sections` belum didukung.
- Inlining dibatasi pada deklarasi array string statis di awal file dengan getter top-level sederhana. Hasilnya membantu pemindaian, bukan bukti perilaku runtime.
- Batas respons: HTML 16 MiB, JavaScript 512 MiB, source map 64 MiB, dan AST 25 MiB. Baris minified panjang tetap dipindai bertahap.
- Jeda request acak 150–300 ms dan header browser mengurangi laju request, tetapi tidak menjamin server menerima pemindaian.

## Lisensi

MIT. Lihat [LICENSE](LICENSE).
