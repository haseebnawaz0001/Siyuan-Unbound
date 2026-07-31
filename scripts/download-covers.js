/**
 * Download header cover images from Pexels
 *
 * Usage:
 *   1. Register for a free API key at https://www.pexels.com/api/
 *   2. Set the environment variable: set PEXELS_API_KEY=your_key   (Windows cmd)
 *                       $env:PEXELS_API_KEY="your_key" (PowerShell)
 *                       export PEXELS_API_KEY=your_key  (Git Bash / Linux / macOS)
 *   3. Run: node scripts/download-covers.js
 *
 * Parameters (optional):
 *   --count=N     Total number of images to download (default 9)
 *   --dir=PATH    Output directory (default app/appearance/covers)
 */

const fs = require("fs");
const path = require("path");
const sharp = require(path.resolve(__dirname, "..", "app", "node_modules", "sharp"));

const API_KEY = process.env.PEXELS_API_KEY;
if (!API_KEY) {
    console.error("❌ Please set the PEXELS_API_KEY environment variable");
    console.error("   Sign up for free: https://www.pexels.com/api/");
    process.exit(1);
}

// Parse command-line arguments
const args = process.argv.slice(2);
const getArg = (name, fallback) => {
    const found = args.find(a => a.startsWith(`--${name}=`));
    return found ? found.split("=")[1] : fallback;
};
const TOTAL = parseInt(getArg("count", "72"), 10);
const OUT_DIR = path.resolve(__dirname, "..", getArg("dir", "app/appearance/covers"));

// Search categories: fetch an equal number of images per category
const SEARCH_QUERIES = [
    { query: "epic mountain landscape photography", label: "自然风景", key: "coverNature" },
    { query: "city night skyline blue hour", label: "城市夜景", key: "coverCityNight" },
    { query: "classical architecture cathedral historic", label: "古典建筑", key: "coverClassicalArchitecture" },
    { query: "cozy reading nook books candle", label: "阅读时光", key: "coverReadingNook" },
    { query: "zen garden minimal calm aesthetic", label: "禅意留白", key: "coverZenMinimal" },
    { query: "architecture light shadow geometry", label: "光影几何", key: "coverLightGeometry" },
    { query: "winding road path journey landscape", label: "路与远方", key: "coverRoadAhead" },
    { query: "autumn fall leaves colorful forest", label: "秋色落叶", key: "coverAutumnLeaves" },
    { query: "neon lights night city vibrant colorful", label: "灯红酒绿", key: "coverNeonNights" },
    { query: "desert sand dune arid landscape", label: "沙漠戈壁", key: "coverDesert" },
    { query: "aurora borealis northern lights sky", label: "极光天象", key: "coverAurora" },
    { query: "morning mist fog valley mountain", label: "晨雾氤氲", key: "coverMistyMorning" },
    { query: "countryside rural farm meadow peaceful", label: "田园乡村", key: "coverCountryside" },
    { query: "tea ceremony calligraphy writing desk", label: "茶道文房", key: "coverTeaCeremony" },
    { query: "calm lake reflection mirror water still", label: "静谧水面", key: "coverStillWater" },
    { query: "chinese garden pavilion architecture", label: "中式园林", key: "coverChineseGarden" },
    { query: "karst mountain mist landscape china", label: "水墨山水", key: "coverInkWashLandscape" },
    { query: "wildlife animal deer fox bird nature", label: "动物生灵", key: "coverWildlife" },
];
const PER_QUERY = Math.ceil(TOTAL / SEARCH_QUERIES.length);

/**
 * Call the Pexels API to search for photos
 */
async function searchPhotos(query, perPage) {
    const url = `https://api.pexels.com/v1/search?query=${encodeURIComponent(query)}&per_page=${perPage}&orientation=landscape&size=medium`;
    const resp = await fetch(url, {
        headers: { Authorization: API_KEY },
    });
    if (!resp.ok) {
        const text = await resp.text();
        throw new Error(`Pexels API request failed (${resp.status}): ${text}`);
    }
    const data = await resp.json();
    return data.photos || [];
}

/**
 * Download a single photo
 */
async function downloadPhoto(photo, outDir, index) {
    // Fetch the original image from Pexels; sharp uniformly crops it to a 2x Retina size
    const width = 2400;
    const height = 800;
    const imgUrl = photo.src.original;

    const filename = `cover_${String(index).padStart(3, "0")}.webp`;
    const filePath = path.join(outDir, filename);

    console.log(`  📥 Downloading: ${filename} ← ${photo.photographer} / Pexels`);

    const imgResp = await fetch(imgUrl);
    if (!imgResp.ok) {
        throw new Error(`Failed to download image (${imgResp.status}): ${imgUrl}`);
    }
    const inputBuffer = Buffer.from(await imgResp.arrayBuffer());

    // Convert to webp
    const outputBuffer = await sharp(inputBuffer)
        .resize(width, height, { fit: "cover" })
        .webp({ quality: 85 })
        .toBuffer();

    fs.writeFileSync(filePath, outputBuffer);

    const kb = (outputBuffer.length / 1024).toFixed(1);
    console.log(`     ✅ ${filename}（${kb} KB）`);

    return {
        file: filename,
        category: photo.alt || "",
        photographer: photo.photographer,
        photographer_url: photo.photographer_url,
        pexels_url: photo.url,
        width,
        height,
    };
}

async function main() {
    console.log(`🎨 Downloading document cover images (${TOTAL} in total)...\n`);

    // Create the output directory
    fs.mkdirSync(OUT_DIR, { recursive: true });

    // Global dedup set (by photo id)
    const seen = new Set();
    let allPhotos = [];

    for (const { query, label, key } of SEARCH_QUERIES) {
        console.log(`🔍 Searching "${label}"...`);
        const photos = await searchPhotos(query, PER_QUERY * 2); // Fetch extra so there's enough margin after dedup
        // Dedup: global ID + photographer within a category
        let categoryPhotos = [];
        const categoryPhotographers = new Set();
        for (const p of photos) {
            if (seen.has(p.id)) continue;
            if (categoryPhotographers.has(p.photographer)) continue; // Avoid repeating a photographer within a category
            seen.add(p.id);
            categoryPhotographers.add(p.photographer);
            p._category = key;
            categoryPhotos.push(p);
        }
        // Take PER_QUERY photos per category
        categoryPhotos = categoryPhotos.slice(0, PER_QUERY);
        allPhotos = allPhotos.concat(categoryPhotos);
        console.log(`   Found ${photos.length}, selected ${categoryPhotos.length} after dedup`);
    }

    if (allPhotos.length < TOTAL) {
        console.warn(`⚠️  Only found ${allPhotos.length} (target ${TOTAL}), downloading all available images`);
    }

    // Download the images
    console.log(`\n📦 Downloading ${allPhotos.length} images to ${OUT_DIR} ...\n`);
    const manifest = [];
    for (let i = 0; i < allPhotos.length; i++) {
        try {
            const entry = await downloadPhoto(allPhotos[i], OUT_DIR, i + 1);
            entry.category = allPhotos[i]._category;
            manifest.push(entry);
        } catch (err) {
            console.error(`   ❌ Download failed: ${err.message}`);
        }
    }

    // Write the manifest
    const manifestPath = path.join(OUT_DIR, "manifest.json");
    fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, "  "), "utf-8");
    console.log(`\n📋 manifest.json generated (${manifest.length} records)`);

    console.log("\n✨ Download complete. The images are in:");
    console.log(`   ${OUT_DIR}`);
    console.log("\n💡 Open the images in a browser to review them");
    console.log("   Once you are happy with them, move on to step two: update Background.ts to wire them into the cover dialog\n");
}

main().catch(err => {
    console.error("❌ 脚本执行失败：", err.message);
    process.exit(1);
});
