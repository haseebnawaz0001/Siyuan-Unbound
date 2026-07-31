<p align="center">
<img alt="SiYuan" src="app/stage/icon.png" width="128">
<br><br>
<a href="https://www.gnu.org/licenses/agpl-3.0.txt"><img src="https://img.shields.io/badge/license-AGPLv3-blue.svg" alt="License: AGPL v3"></a>
</p>

<p align="center">
<a href="README.md">English</a> | <a href="README.zh-CN.md">中文</a> | <a href="README.ja.md">日本語</a> | <strong>Türkçe</strong>
</p>

---

## Bu proje nedir

**SiYuan Unbound, [SiYuan](https://github.com/siyuan-note/siyuan)'ın bir fork'udur** — blok düzeyinde referanslara ve Markdown WYSIWYG düzenlemeye sahip, gizliliği ön planda tutan kişisel bir bilgi yönetim sistemi. Bu bir yeniden yazım değil; kodun neredeyse tamamı upstream'e ait ve fork, upstream'den merge almaya devam edebilecek kadar yakın tutuluyor.

Dört şey farklı:

- **Senkronizasyon, kendi sağladığın depolama için abonelik gerektirmeden çalışır.** S3 uyumlu nesne depolama, WebDAV ve yerel bir dosya sistemi dizini, hesap açmaya gerek kalmadan senkronize olur. SiYuan'ın kendi barındırdığı hizmetler kilitli kalmaya devam ediyor — aşağıya bak.
- **Telemetri kaldırıldı.** Donanım cihaz parmak izi yok, otomatik duyuru çekimi yok.
- **Varsayılan dil İngilizce**, kaynak kod yorumları da İngilizce. Dil değiştirici hâlâ çalışıyor ve her çeviri hâlâ dağıtımda.
- **Çakışan belgeler blok blok birleştirilir**, bir tarafın her seferinde kaybetmesi yerine.

[`docs/FORK.md`](docs/FORK.md) her farklılığı, neden yapıldığını ve upstream'e karşı rebase yapıldığında nerede çakışma çıkacağını kaydeder.

Bu resmi olmayan bir fork'tur. SiYuan projesi tarafından desteklenmez, onaylanmaz veya onunla ilişkili değildir; hiçbir uygulama mağazası listelemesi, yayınlanmış Docker görüntüsü veya destek kanalı yoktur. Lisans değişmedi: AGPL-3.0.

![Editing and block references](screenshots/feature0.png)

![Database views](screenshots/feature5-1.png)

## Özellikler

- İçerik bloğu
  - Blok düzeyinde referans ve çift yönlü bağlantılar
  - Özel nitelikler
  - Gömülü SQL sorgusu
  - `siyuan://` protokolü
- Editör
  - Blok tabanlı yapı
  - Markdown WYSIWYG
  - Liste taslağı
  - Blok yakınlaştırma
  - Milyon kelimelik büyük belge düzenleme
  - Matematiksel formüller, grafikler, akış diyagramları, Gantt diyagramları, zaman diyagramları, notalar vb.
  - Web kırpma
  - PDF açıklama bağlantısı
- Dışa aktarım
  - Blok referansı ve gömme
  - Varlıklarıyla birlikte standart Markdown
  - PDF, Word ve HTML
  - WeChat MP, Zhihu ve Yuque'a kopyalama
- Veritabanı
  - Tablo görünümü
- Aralıklı tekrar (flashcard)
- OpenAI API üzerinden yapay zeka yazma ve Soru-Cevap sohbeti
- Tesseract OCR
- Çok sekmeli görünüm, sürükle-bırak ile ekran bölme
- Şablon parçacığı
- JavaScript/CSS parçacığı
- Kendi S3 / WebDAV / yerel depolamana senkronizasyon, hesap gerektirmez
- Docker dağıtımı
- Topluluk pazaryeri

### Hâlâ ücretli olan

SiYuan'ın kendi sunucularında çalışan her şey, bilinçli olarak kilitli bırakıldı: resmi bulut senkronizasyonu, bulut görsel ve varlık barındırma, bulut hatırlatıcılar, dışa aktarımda CDN varlık dönüştürme ve Liandi yayınlama. AGPL-3.0, kendi kopyanı değiştirmeni ve çalıştırmanı kapsar; başkasının altyapısını ücretsiz kullanmanı kapsamaz. Bunları istiyorsan [Fiyatlandırma](https://b3log.org/siyuan/en/pricing.html) sayfasına bak.

## Bir build edinmek

İndirme yok. Kendin build et.

```bash
git clone git@github.com:haseebnawaz0001/Siyuan-Unbound.git
cd Siyuan-Unbound

cd app && pnpm install && pnpm run install:electron && pnpm run build && cd ..

cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..

./app/kernel/SiYuan-Kernel serve
```

Go 1.26, Node 24, pnpm 11.12.0 ve bir C derleyicisine ihtiyacın var. Çekirdek, arayüzü HTTP üzerinden sunar; yani bu son satır çalışan bir kurulumdur — yükleyici gerekmez.

Masaüstü yükleyicileri, çapraz derleme, Docker, mobil ve bu depodaki beş build yolunun birbiriyle nerede çeliştiği için **[`docs/BUILD.md`](docs/BUILD.md)** dosyasını oku.

### Ya da CI'a build ettir

`.github/workflows/cd.yml` dosyasının sahip kısıtlaması yok, bu yüzden bir fork üzerinde de çalışır. `*-alpha*`, `*-beta*` veya `*-rc*` ile eşleşen bir etiket (tag) push et ya da workflow'u elle tetikle; Windows, macOS ve Linux yükleyicilerini build edip kendi deponda bir Release'e ekler.

İki uyarı: o workflow'daki Android işi senin kontrolünde olmayan bir depoya push yapar ve başarısız olur — masaüstü çıktıları bundan etkilenmez — ve bu yol bu fork üzerinde henüz denenmedi.

## Kendi sunucunda barındırma

Görüntüyü build et ve çalıştır:

```bash
docker build -t siyuan-unbound .
```

Bu fork hiçbir görüntü yayınlamıyor ve yayınlayamaz: `.github/workflows/dockerimage.yml`, upstream sahibine kilitli olduğu için yayın işi burada asla çalışmaz. **[`docs/DEPLOY.md`](docs/DEPLOY.md)** dosyası konteyneri çalıştırmayı, Docker Compose'u, Unraid'i ve TrueNAS'ı ve önemli bir uyarıyı kapsıyor — Docker build'i `sqlcipher` build etiketini içermiyor, yani şifreli not defterlerinin bu görüntüde çalıştığı varsayılmamalı.

## Belgeler

| Belge | Neyi yanıtlar |
|---|---|
| [FORK.md](docs/FORK.md) | Bu fork upstream'den nasıl farklılaşıyor ve neden |
| [SYNC.md](docs/SYNC.md) | Kendi S3, WebDAV veya yerel depolamana karşı senkronizasyon kurulumu |
| [BUILD.md](docs/BUILD.md) | Kaynak koddan build, her platform için |
| [DEPLOY.md](docs/DEPLOY.md) | Docker, Compose, Unraid, TrueNAS |
| [WORKSPACE.md](docs/WORKSPACE.md) | Bir çalışma alanının diskte neye benzediği |
| [SY-FORMAT.md](docs/SY-FORMAT.md) | `.sy` belge formatı |
| [ENCRYPTED-NOTEBOOK.md](docs/ENCRYPTED-NOTEBOOK.md) | Şifreli not defterleri nasıl çalışır |
| [API.md](docs/API.md) | Çekirdeğin HTTP API'si |
| [CONTRIBUTING.md](.github/CONTRIBUTING.md) | Geliştirme kurulumu ve kurallar |
| [AGENTS.md](AGENTS.md) | Depo rehberi, burada bir kodlama ajanının uyması gereken kurallar dahil |

## Komut satırı arayüzü

Çekirdek ikili dosyası aynı zamanda CLI'dır ve çalışma alanı verisini doğrudan okur — çalışan bir sunucu gerekmez.

```bash
# Tüm not defterlerini listele
siyuan notebook list -w ~/SiYuan

# JSON çıktısıyla tam metin arama
siyuan search "keyword" -w ~/SiYuan -f json

# Varlık dosyalarının içinde arama (PDF/Word/Excel/txt vb.)
siyuan search "phrase" --asset -w ~/SiYuan
siyuan search "phrase" --asset --ext pdf --ext docx -w ~/SiYuan

# Bir belgeyi Markdown olarak dışa aktar
siyuan export md --id <block-id> -w ~/SiYuan
```

| Kategori | Komutlar |
|----------|----------|
| Defterler ve Belgeler | `notebook`, `document`, `dailynote` — CRUD ve günlük notlar |
| İçerik | `block`, `attr`, `outline` — blok okuma/yazma, nitelikler, ana hat |
| Meta Veri | `tag`, `bookmark`, `template` — etiketler, yer imleri, şablon parçacıkları |
| Sorgular | `search`, `sql` — tam metin, anlamsal, varlık içeriği ve SQL sorguları |
| Referanslar | `ref` — geri bağlantılar ve bahsetmeler |
| İçe/Dışa Aktarma | `export`, `import`, `inbox` — Markdown, HTML, preview, Word, .sy.zip, Data, bulut gelen kutusu |
| Veri Yönetimi | `repo`, `history`, `sync` — anlık görüntüler, sürümler, bulut senkronizasyonu |
| Araçlar | `asset`, `file` — kaynaklar ve dosya sistemi |
| Veritabanı | `database` — öznitelik görünümü yönetimi |
| Sunucu | `serve` — çekirdek HTTP sunucusunu başlat |
| Çalışma Alanı ve Sistem | `workspace`, `system` — listeleme, inceleme, sistem bilgisi |

Komut ağacının tamamı için `siyuan --help` çalıştır. Script dostu çıktı için (varsayılan `-f table` yerine) `-f json` kullan. Değişiklik yapan komutların çoğu, uygulamadan önce değişiklikleri önizlemek için `--dry-run` seçeneğini de destekler.

İkili dosya `<install-dir>/resources/kernel/SiYuan-Kernel` konumundadır, ya da nereye build ettiysen orada. Ona `siyuan` olarak ulaşmak için `PATH`'ine symlink'le:

```bash
ln -s <install-dir>/resources/kernel/SiYuan-Kernel /usr/local/bin/siyuan
```

## Mimari ve ekosistem

| Proje | Rol |
|---|---|
| [lute](https://github.com/88250/lute) | Editör motoru — Markdown/`.sy` AST'si |
| [dejavu](third_party/dejavu) | Veri deposu ve senkronizasyon motoru — **depoya gömülü fork**, bkz. [FORK.md](docs/FORK.md) §4 |
| [riff](https://github.com/siyuan-note/riff) | Aralıklı tekrar zamanlayıcısı |
| [bazaar](https://github.com/siyuan-note/bazaar) | Topluluk pazaryeri |
| [petal](https://github.com/siyuan-note/petal) | Eklenti API'si |
| [chrome](https://github.com/siyuan-note/siyuan-chrome) | Web kırpma uzantısı |
| [android](https://github.com/siyuan-note/siyuan-android) / [ios](https://github.com/siyuan-note/siyuan-ios) / [harmony](https://github.com/siyuan-note/siyuan-harmony) | gomobile çekirdeğini saran mobil uygulamalar |

`dejavu` dışında bunlar, bu fork'un değiştirmeden kullandığı upstream tarafından bakımı yapılan projeler. Mobil uygulamalar ve uzantı çekirdekle HTTP API'si üzerinden konuşur, yani bu fork'a karşı da çalışırlar, ama bunları build etmek burada kapsam dışı — bkz. [`docs/BUILD.md`](docs/BUILD.md) §6.

## SSS

### SiYuan verileri nasıl saklar?

Veri, çalışma alanının data klasöründe saklanır:

- `assets`, eklenen tüm varlıkları kaydetmek için kullanılır
- `emojis`, emoji görsellerini kaydetmek için kullanılır
- `snippets`, kod parçacıklarını kaydetmek için kullanılır
- `storage`, sorgu koşullarını, düzenleri ve bilgi kartlarını vb. kaydetmek için kullanılır
- `templates`, şablon parçacıklarını kaydetmek için kullanılır
- `widgets`, widget'ları kaydetmek için kullanılır
- `plugins`, eklentileri kaydetmek için kullanılır
- `public`, genel verileri kaydetmek için kullanılır
- Geri kalan klasörler, kullanıcı tarafından oluşturulan not defteri klasörleridir; not defteri klasöründeki `.sy` uzantılı dosyalar belge verisini saklamak için kullanılır ve veri formatı JSON'dur

[`docs/WORKSPACE.md`](docs/WORKSPACE.md) tam referanstır.

### Üçüncü taraf senkronizasyon diskiyle veri senkronizasyonu destekleniyor mu?

Hayır — çalışma alanına Dropbox, OneDrive veya benzeri bir klasör senkronizasyon aracını yöneltmek veriyi bozabilir.

Kendi S3, WebDAV veya yerel dosya sistemi depolamanı bağlamak tamamen farklı bir şeydir ve abonelik gerektirmeden tam olarak desteklenir. Çekirdek, canlı dosyaları senkronize etmek yerine değişmez, içerik adresli bir depo yazar. Bkz. [`docs/SYNC.md`](docs/SYNC.md).

### Bu açık kaynak mı?

Evet, upstream ile aynı şekilde AGPL-3.0. Bunu başkaları için bir ağ hizmeti olarak çalıştırırsan AGPL §13'ün geçerli olduğunu unutma — onlara kaynak kodu borçlusun.

Upstream'in kendi depoları, hepsi ayrı projeler: [kullanıcı arayüzü ve çekirdek](https://github.com/siyuan-note/siyuan), [Android](https://github.com/siyuan-note/siyuan-android), [iOS](https://github.com/siyuan-note/siyuan-ios), [HarmonyOS](https://github.com/siyuan-note/siyuan-harmony), [Chrome kırpma uzantısı](https://github.com/siyuan-note/siyuan-chrome).

### Nasıl yükseltirim?

Otomatik güncelleme yok — o mekanizma upstream'in yayın altyapısına işaret ediyor ve bu fork'ta o altyapı yok. Çek ve yeniden build et:

```bash
git pull
cd app && pnpm install && pnpm run build && cd ..
cd kernel && CGO_ENABLED=1 go build -tags "fts5 sqlcipher" -o "../app/kernel/SiYuan-Kernel" && cd ..
```

Upstream'in değişikliklerini de almak için önce onları merge et: `git fetch upstream && git merge upstream/master`. Yorumlarda çakışma bekle — nerede çıkacağını [`docs/FORK.md`](docs/FORK.md) listeler.

### Bazı bloklar (örneğin liste öğelerindeki paragraf blokları) blok simgesini bulamıyorsa ne yapmalıyım?

Liste öğesinin altındaki ilk alt blok için blok simgesi gösterilmez. İmleci bu bloğun içine getirip <kbd>Ctrl+/</kbd> ile blok menüsünü tetikleyebilirsin.

### Veri deposu anahtarı kaybolursa ne yapmalıyım?

- Veri deposu anahtarı daha önce birden fazla cihazda doğru şekilde başlatıldıysa, anahtar tüm cihazlarda aynıdır ve <kbd>Ayarlar</kbd> - <kbd>Hesap ve Senkronizasyon</kbd> - <kbd>Yerel Veri Deposu</kbd> - <kbd>Veri deposu anahtarı</kbd> - <kbd>Anahtar dizgesini kopyala</kbd> yolundan alınabilir
- Daha önce doğru şekilde yapılandırılmadıysa (örneğin birden fazla cihazdaki anahtarlar tutarsızsa) veya tüm cihazlar erişilemezse ve anahtar dizgesi elde edilemiyorsa, aşağıdaki adımları izleyerek anahtarı sıfırlayabilirsin:

  1. Veriyi elle yedekle; bunun için <kbd>Verileri Dışa Aktar</kbd> seçeneğini kullanabilir ya da dosya sisteminde <kbd>workspace/data/</kbd> klasörünü doğrudan kopyalayabilirsin
  2. <kbd>Ayarlar</kbd> - <kbd>Hesap ve Senkronizasyon</kbd> - <kbd>Yerel Veri Deposu</kbd> - <kbd>Veri deposu anahtarı</kbd> - <kbd>Veri deposunu sıfırla</kbd>
  3. Veri deposu anahtarını yeniden başlat. Anahtarı bir cihazda başlattıktan sonra, diğer cihazlar anahtarı içe aktarır
  4. Bulut yeni senkronizasyon dizinini kullanır, eski senkronizasyon dizini artık kullanılamaz ve silinebilir
  5. Mevcut bulut anlık görüntüleri (snapshot) artık kullanılamaz ve silinebilir

## Teşekkür

SiYuan, [b3log](https://github.com/siyuan-note) ve katkıda bulunanlarının emeğidir. Bu fork, o çalışma iyi ve açık olduğu için var; yazılıma dair tüm övgü upstream'e aittir ve [`docs/FORK.md`](docs/FORK.md) içindeki değişikliklerin yol açtığı herhangi bir hata onlara değil buraya aittir. Lütfen bu fork'la ilgili sorunları upstream projesine bildirme.

SiYuan birçok açık kaynak projeye bağımlıdır — bkz. `kernel/go.mod` ve `app/package.json`.
