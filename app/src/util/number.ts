// Abbreviates a number into a short k/M-suffixed format, used for displaying counts such as bazaar downloads, stars, and issues
export const formatCount = (n: number | string) => {
    const num = typeof n === "string" ? parseFloat(n) : n;
    if (!Number.isFinite(num)) {
        // The backend may return an empty string or a non-numeric value; return it as-is to avoid displaying NaN
        return n;
    }
    if (num < 1000) {
        return n.toString();
    }
    let value: number;
    let suffix: string;
    if (num < 1000000) {
        value = num / 1000;
        suffix = "k";
    } else {
        value = num / 1000000;
        suffix = "M";
    }
    // Keep one decimal place, dropping a meaningless trailing .0 (e.g. 1.0k -> 1k)
    return (value.toFixed(1).replace(/\.0$/, "")) + suffix;
};
