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

Memerlukan **Go 1.24+**. Pasang atau perbarui sekali:

```sh
go install github.com/Hzkun001/jsmap-pro@v0.1.3
```

Lalu pindai website Anda:

```sh
jsmap-pro example.com
```

Ganti `example.com` dengan website Anda yang memuat aset JavaScript. Hostname otomatis menggunakan HTTPS; untuk server HTTP lokal, tulis lengkap `http://localhost:8080`.

Laporan tersimpan di `jsmap-output/summary.json` dan `jsmap-output/summary.txt`. Untuk memilih folder lain atau menjalankan pemindaian berikutnya:

```sh
jsmap-pro example.com -o hasil
```

Opsi boleh diletakkan sebelum atau sesudah target. Folder output harus baru atau kosong. Jalankan `jsmap-pro -h` (atau cukup `jsmap-pro`) untuk melihat bantuan dan contoh.

> [!TIP]
> **Command not found setelah `go install`?** Go biasanya menyimpan binary di `$(go env GOPATH)/bin`. Untuk shell saat ini, jalankan `export PATH="$(go env GOPATH)/bin:$PATH"`. Tambahkan baris itu ke `~/.bashrc` atau `~/.zshrc` agar tetap berlaku. Jika Anda mengatur `GOBIN`, tambahkan direktori tersebut.

### Tanpa Go

Unduh arsip sesuai OS dan CPU dari [Releases](https://github.com/Hzkun001/jsmap-pro/releases/latest), ekstrak, lalu buka terminal di direktori hasil ekstraksi:

```sh
./jsmap-pro example.com
```

Di Windows, gunakan `.\jsmap-pro.exe example.com` melalui PowerShell. Perintah langsung `jsmap-pro` dapat dipakai setelah direktori binary masuk ke PATH.

### Dari source

```sh
git clone https://github.com/Hzkun001/jsmap-pro.git
cd jsmap-pro
go run . example.com
```

Untuk membuat binary lokal yang bisa dijalankan berulang kali:

```sh
go build -ldflags="-s -w" -o jsmap-pro .
./jsmap-pro example.com
```

### Coba secara offline

Dari salinan repo, contoh ini membuat file JavaScript kecil dan memindainya tanpa koneksi jaringan:

```sh
mkdir -p demo
printf 'crypto.subtle.encrypt(key, data);\n' > demo/app.js
go run . -d demo -o hasil-demo
```

Gunakan folder `-o` baru saat mengulang contoh. Aset mentah disimpan di `raw/`, sumber yang dipulihkan di `unpacked/`, dan kode hasil normalisasi di `analyzed/` jika tersedia.

## Mode input

Pilih satu sumber input untuk setiap pemindaian:

| Mode | Contoh | Kegunaan |
|:--|:--|:--|
| Website | `-u https://example.com` | Temukan aset dari tag `<script>` dan referensi `.js` statis di inline script. |
| Daftar URL | `-l urls.txt` | Pindai URL JavaScript yang sudah dikumpulkan. |
| HAR offline | `-har capture.har` | Analisis respons JavaScript dari HTTP Archive tanpa menghubungi server. |
| Direktori lokal | `-d ./assets` | Analisis file `.js` di disk, termasuk subdirektori. |

```sh
jsmap-pro -l urls.txt -c 20 -o hasil-url
jsmap-pro -har capture.har -o hasil-har
jsmap-pro -d ./assets -o hasil-lokal
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

Jika JavaScript diakhiri `//# sourceMappingURL=...`, jsmap-pro memeriksa map dan merekonstruksi `sourcesContent` ke `unpacked/`. Status **Fully Reconstructed via SourceMap** hanya dipakai jika semua sumber berhasil ditulis. Konten sumber yang tidak tersedia dan path yang tidak aman akan dicatat.

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
go build -ldflags="-s -w" -o jsmap-pro .
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
