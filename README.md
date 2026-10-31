# jsmap-pro

CLI Go untuk menemukan aset JavaScript dari halaman utama, mengunduhnya secara streaming, mencari pola kriptografi/signature, serta merekonstruksi sumber dari source map publik.

## Build dan penggunaan

Memerlukan Go 1.24 atau lebih baru saat build (API `os.Root` menjaga ekstraksi tetap di bawah direktori output). Parser DOM menggunakan `golang.org/x/net/html` dan parser JS menggunakan `github.com/tdewolff/parse/v2/js`; hasil build adalah satu binary tanpa runtime Go atau browser tambahan. Go 1.21 belum didukung; minimum ini akan diturunkan setelah isolasi file aman kompatibel Go 1.21 tersedia.

```sh
go build -ldflags="-s -w" -o jsmap-pro ./cmd/jsmap && ./jsmap-pro -u https://example.com -c 10 -o hasil
```

Input daftar URL JS langsung:

```sh
./jsmap-pro -l urls.txt -c 20 -o hasil-list -timeout 60s
```

`urls.txt` berisi satu URL HTTP(S) absolut per baris. Baris kosong dan komentar yang diawali `#` diabaikan. URL dideduplikasi; fragment dihapus dan query string dipertahankan.

| Flag | Default | Arti |
| --- | --- | --- |
| `-u` | — | URL website |
| `-l` | — | File daftar URL JavaScript langsung |
| `-har` | — | File HAR offline |
| `-d` | — | Direktori JavaScript offline |
| `-c` | `10` | Jumlah worker untuk unduh, analisis, dan source map |
| `-o` | `jsmap-output` | Direktori baru atau kosong; isi yang sudah ada tidak ditimpa |
| `-timeout` | `30s` | Timeout setiap request, termasuk pembacaan body |

## Hasil

```text
hasil/
  raw/
    <hash-url>-app.js
    <hash-url>-app.js.map
  unpacked/
    <hash-url>-app/
      src/...
  summary.json
  summary.txt
```

- Penemuan memakai DOM `<script src>`, termasuk URL aset tanpa ekstensi `.js`, serta referensi literal `.js` pada inline script. Resolusi mengikuti URL halaman sesudah redirect dan `<base href>` jika ada. URL CDN juga diunduh.
- `io.TeeReader` menyimpan byte JS apa adanya sementara `bufio.Scanner` memindai potongan 64 KiB dengan overlap. Satu baris minified besar tidak dimuat sekaligus. Nomor baris mengikuti LF/CRLF; setiap kategori dicatat sekali per baris, dengan cuplikan di sekitar kecocokan pertama.
- Kategori: WebCrypto (`encrypt`, `decrypt`, `sign`, `verify`, `importKey`), Third-party Crypto (`CryptoJS`, `forge.cipher`, `JSEncrypt`, `U2FsdGVkX1`), Custom Header / Signature (`X-Signature`, `X-Sign`, `X-Timestamp`, `X-Request-Id`), Base64 Helper (`atob`, `btoa`).
- Anotasi `//# sourceMappingURL=...` atau `/*# sourceMappingURL=... */` pada baris terakhir yang tidak kosong diresolusikan terhadap URL JS sesudah redirect. Map diunduh hanya setelah HEAD mengembalikan HTTP 200, lalu GET juga wajib HTTP 200.
- Source map v3 dengan `sources`, `sourceRoot`, dan `sourcesContent` diekstrak; isi sumber kosong tetap menjadi file kosong. Prefix seperti `webpack:///./src/` dinormalisasi menjadi `src/`. Setiap bundle mendapat direktori sendiri, dengan proteksi traversal dan symlink melalui `os.Root`. Path yang keluar dari direktori, drive Windows, serta benturan nama file ditolak dan dicatat.
- **Fully Reconstructed via SourceMap** berarti semua `sourcesContent` berhasil ditulis. Nilai `null`/konten yang tidak tersedia menghasilkan status parsial; tool tidak mengarang isi sumber atau mengambilnya dari URL sumber lain. Map invalid dan kegagalan HTTP ditulis sebagai error.
- Progres dan temuan memakai ANSI color di STDOUT. Cuplikan dan input eksternal di-escape agar kontrol terminal dari aset tidak dijalankan. Laporan teks tidak mengandung ANSI.
- Exit `0`: seluruh aset selesai, tanpa map bermasalah; ketiadaan anotasi map bukan error. Exit `1`: input/IO/network gagal atau ada rekonstruksi parsial. Error per aset tidak menghentikan aset lain; laporan tetap ditulis. Exit `2`: kesalahan parsing flag. Ctrl-C membatalkan request dan menyimpan hasil/error aset yang telah masuk antrean.

## Batas cakupan

Crawling bersifat statis dan hanya membaca halaman utama. Tidak ada eksekusi browser, navigasi link halaman lain, pembacaan import rekursif, atau evaluasi rumus pembentuk chunk webpack. Referensi literal bisa menghasilkan false positive. Regex menunjukkan lokasi pola, bukan bukti kerentanan, atau pemetaan nama fungsi asli dari VLQ `mappings`.

Map inline `data:` dan indexed map `sections` belum didukung. Server yang menolak HEAD dianggap tidak tersedia walaupun GET mungkin berhasil. Anotasi map diperiksa pada bagian akhir file (maksimal 8 KiB, mengabaikan baris kosong di akhir), termasuk komentar blok beberapa baris.

Batas per respons: HTML 16 MiB, JS 512 MiB, map 64 MiB (ukuran sesudah dekompresi HTTP). HTML diparse menjadi DOM dan JSON source map didekode di memori; konsumsi memori map meningkat mengikuti concurrency. Temuan disimpan untuk laporan, sedangkan byte JS tetap dialirkan ke disk. Batas ini tidak membatasi total ukuran seluruh unduhan.

## Verifikasi lokal

```sh
go test -race ./...
go vet ./...
```

Tes memakai server HTTP lokal untuk crawling, redirect, `<base href>`, deduplikasi, worker pool, rekonstruksi lengkap/parsial, HEAD gagal, JSON invalid, timeout, unduhan terputus, baris minified >10 MiB, pola lintas potongan, serta proteksi traversal dan symlink. Tidak membutuhkan target internet.

Referensi: [parser HTML resmi Go](https://pkg.go.dev/golang.org/x/net/html), [format source map ECMA-426](https://tc39.es/ecma426/).

## Input offline dan inliner AST

Empat mode input `-u`, `-l`, `-har`, dan `-d` saling eksklusif:

```sh
./jsmap-pro -har capture.har -o hasil-har
./jsmap-pro -d ./assets -o hasil-local
```

HAR dipilih berdasarkan MIME yang mengandung `javascript` atau path URL `.js` (query string diperbolehkan). Isi `response.content.text`, termasuk encoding `base64`, disimpan ke `raw/`. Body yang tidak disertakan di HAR dilaporkan sebagai error. Duplikat URL memakai body pertama yang tersedia. Ukuran file HAR dibatasi 512 MiB dan JSON dimuat di memori.

Mode direktori membaca `.js` secara rekursif dan melewati symlink. Kedua mode offline tidak mengirim request HTTP: source map hanya dicari di respons HAR atau di dalam direktori input. Map yang tidak tersedia dilaporkan sebagai unavailable; map lokal yang keluar dari direktori input ditolak.

HTTP kini memakai User-Agent desktop, `Sec-Fetch-Dest: script`, `Sec-Fetch-Mode: no-cors`, dan `Accept-Language`. Setiap pemanggilan request diberi `time.Sleep` acak 150–300 ms, termasuk HEAD/GET map. Ini jeda per worker, bukan batas request global; redirect otomatis HTTP tidak memiliki jeda terpisah. Header dan jitter tidak menjamin server tidak memblokir request.

Scanner memakai buffer awal 1 MiB dan maksimum 25 MiB, dengan splitter 64 KiB dan overlap yang sudah ada. Baris minified di atas 25 MiB tetap bisa dipindai tanpa menjadi satu token besar.

`inliner.go` memakai parser `github.com/tdewolff/parse/v2/js` untuk mengganti pemanggilan getter statis, misalnya:

```js
const strings = ["X-Signature"];
function get(i) { return strings[i]; }
send(get(0)); // hasil analisis: send("X-Signature");
```

AST diserialisasi ke `analyzed/<nama-aset>.js`, kemudian dipindai lagi. Temuan dari hasil transformasi mengacu pada file dan nomor baris `analyzed/`; temuan asli tetap mengacu pada `raw/`. `analysis_note` di JSON/TXT mencatat jumlah penggantian atau alasan tahap AST dilewati.

Inliner dibatasi pada array string dalam deklarasi statis di awal file dan function getter top-level satu parameter yang langsung mengembalikan `array[index]`. Akses binding mengikuti scope parser. Indeks dinamis, spread, getter async/generator, alias, mutasi, serta penggunaan `eval`, `Function`, `with`, `this`, atau objek global umum dilewati. Ini bantuan analisis statis pada satu chunk, bukan pembuktian perilaku lintas-script atau deobfuscator umum. File yang gagal diparse atau lebih besar dari 25 MiB tetap mendapat scan raw; batas AST dicatat. JavaScript tidak dieksekusi.
