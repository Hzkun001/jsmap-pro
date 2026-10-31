# jsmap-crawler

CLI Go untuk menemukan aset JavaScript dari halaman utama, mengunduhnya secara streaming, mencari pola kriptografi/signature, serta merekonstruksi sumber dari source map publik.

## Build dan penggunaan

Memerlukan Go 1.26 atau lebih baru saat build. Parser DOM menggunakan `golang.org/x/net/html`; hasil build adalah satu binary tanpa runtime Go atau browser tambahan.

```sh
go build -ldflags="-s -w" -o jsmap-crawler . && ./jsmap-crawler -u https://example.com -c 10 -o hasil
```

Input daftar URL JS langsung:

```sh
./jsmap-crawler -l urls.txt -c 20 -o hasil-list -timeout 60s
```

`urls.txt` berisi satu URL HTTP(S) absolut per baris. Baris kosong dan komentar yang diawali `#` diabaikan. URL dideduplikasi; fragment dihapus dan query string dipertahankan.

| Flag | Default | Arti |
| --- | --- | --- |
| `-u` | — | URL website, tidak boleh bersamaan dengan `-l` |
| `-l` | — | File daftar URL JavaScript langsung |
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
  report.json
  report.txt
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

Crawling bersifat statis dan hanya membaca halaman utama. Tidak ada eksekusi browser, navigasi link halaman lain, pembacaan import rekursif, atau evaluasi rumus pembentuk chunk webpack. Referensi literal bisa menghasilkan false positive. Regex menunjukkan lokasi pola, bukan bukti kerentanan, analisis AST, atau pemetaan nama fungsi asli dari VLQ `mappings`.

Map inline `data:` dan indexed map `sections` belum didukung. Server yang menolak HEAD dianggap tidak tersedia walaupun GET mungkin berhasil. Anotasi map diperiksa pada bagian akhir file (maksimal 8 KiB, mengabaikan baris kosong di akhir), termasuk komentar blok beberapa baris.

Batas per respons: HTML 16 MiB, JS 512 MiB, map 64 MiB (ukuran sesudah dekompresi HTTP). HTML diparse menjadi DOM dan JSON source map didekode di memori; konsumsi memori map meningkat mengikuti concurrency. Temuan disimpan untuk laporan, sedangkan byte JS tetap dialirkan ke disk. Batas ini tidak membatasi total ukuran seluruh unduhan.

## Verifikasi lokal

```sh
go test -race ./...
go vet ./...
```

Tes memakai server HTTP lokal untuk crawling, redirect, `<base href>`, deduplikasi, worker pool, rekonstruksi lengkap/parsial, HEAD gagal, JSON invalid, timeout, unduhan terputus, baris minified >10 MiB, pola lintas potongan, serta proteksi traversal dan symlink. Tidak membutuhkan target internet.

Referensi: [parser HTML resmi Go](https://pkg.go.dev/golang.org/x/net/html), [format source map ECMA-426](https://tc39.es/ecma426/).
