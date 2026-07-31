import { isMobile } from "./functions";

const COOKIE_MAX_AGE = 34560000;

const readCookieValue = (name: string, maxAge: number = COOKIE_MAX_AGE, path: string = "/"): string | undefined => {
    const segments = document.cookie.split(";");
    for (const segment of segments) {
        const trimmed = segment.trim();
        if (!trimmed) {
            continue;
        }
        const eq = trimmed.indexOf("=");
        if (eq === -1) {
            continue;
        }
        const key = trimmed.slice(0, eq).trim();
        if (key !== name) {
            continue;
        }
        let value = trimmed.slice(eq + 1).trim();
        try {
            value = decodeURIComponent(value);
        } catch {
            // Keep the original value
        }
        refreshCookie(name, value, maxAge, path);
        return value;
    }
    return undefined;
};

const refreshCookie = (name: string, value: string, maxAge: number = COOKIE_MAX_AGE, path: string = "/") => {
    document.cookie = name + "=" + value + ";path=" + path + ";max-age=" + maxAge;
};

export const desktopModeCookie = {
    read: () => {
        const raw = readCookieValue("siyuan-desktop-mode");
        return raw === "true" || (raw !== "false" && !isMobile()); // Accounts for both the case where the cookie exists and where it does not
    },
    set: (enabled: boolean) => {
        document.cookie = "siyuan-desktop-mode=" + (enabled ? "true" : "false") + ";path=/;max-age=" + COOKIE_MAX_AGE;
    },
    remove: () => {
        document.cookie = "siyuan-desktop-mode=;path=/;max-age=0";
    },
};
