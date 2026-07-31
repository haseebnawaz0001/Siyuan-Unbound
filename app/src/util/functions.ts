const CONTAINER_BACKEND_SET = new Set(["docker", "ios", "android", "harmony"]);

export const isKernelInContainer = (): boolean => {
    return CONTAINER_BACKEND_SET.has(window.siyuan.config.system.container);
};

export const isMobile = () => {
    return document.getElementById("sidebar") ? true : false;
};

// "windows" | "linux" | "darwin" | "docker" | "android" | "ios" | "harmony"
export const getBackend = () => {
    if (isKernelInContainer()) {
        return window.siyuan.config.system.container;
    } else {
        return window.siyuan.config.system.os;
    }
};

// "desktop" | "desktop-window" | "mobile" | "browser-desktop" | "browser-mobile"
export const getFrontend = () => {
    /// #if MOBILE
    if (window.navigator.userAgent.startsWith("SiYuan/")) {
        return "mobile";
    } else {
        return "browser-mobile";
    }
    /// #else
    if (window.navigator.userAgent.startsWith("SiYuan/")) {
        if (isWindow()) {
            return "desktop-window";
        }
        return "desktop";
    } else {
        return "browser-desktop";
    }
    /// #endif
};

export const isWindow = () => {
    return document.getElementById("toolbar") ? false : true;
};

export const isTouchDevice = () => {
    return ("ontouchstart" in window) && navigator.maxTouchPoints > 1;
};

export const isArrayEqual = (arr1: string[], arr2: string[]) => {
    return arr1.length === arr2.length && arr1.every((item) => arr2.includes(item));
};

export const getRandom = (min: number, max: number) => {
    return Math.floor(Math.random() * (max - min + 1)) + min; // Inclusive of both max and min
};

export const getSearch = (key: string, link = window.location.search) => {
    const params = link.substring(link.indexOf("?"));
    const hashIndex = params.indexOf("#");
    // REF https://developer.mozilla.org/zh-CN/docs/Web/API/URLSearchParams
    const urlSearchParams = new URLSearchParams(params.substring(0, hashIndex >= 0 ? hashIndex : undefined));
    return urlSearchParams.get(key);
};

/**
 * Determines whether this is a mobile or browser environment
 */
export const isBrowser = () => {
    /// #if BROWSER
    return true;
    /// #else
    return false;
    /// #endif
};

export const isDynamicRef = (text: string) => {
    return /^\(\(\d{14}-\w{7} '.*'\)\)$/.test(text);
};

export const isFileAnnotation = (text: string) => {
    return /^<<assets\/.+\/\d{14}-\w{7} ".+">>$/.test(text);
};

export const isValidCustomAttrName = (name: string) => {
    return /^[a-z][\-0-9a-z]*$/.test(name);
};

// REF https://developer.mozilla.org/zh-CN/docs/Web/JavaScript/Reference/Global_Objects/eval
export const looseJsonParse = (text: string) => {
    return Function(`"use strict";return (${text})`)();
};

export const objEquals = (a: any, b: any): boolean => {
    if (a === b) return true;
    if (typeof a === "number" && isNaN(a) && typeof b === "number" && isNaN(b)) return true;
    if (a instanceof Date && b instanceof Date) return a.getTime() === b.getTime();
    if (!a || !b || (typeof a !== "object" && typeof b !== "object")) return a === b;
    if (a.prototype !== b.prototype) return false;
    const keys = Object.keys(a);
    if (keys.length !== Object.keys(b).length) return false;
    return keys.every(k => objEquals(a[k], b[k]));
};

export const duplicateNameAddOne = (name: string) => {
    if (!name) {
        return "";
    }

    const nameMatch = name.match(/^(.*) \((\d+)\)$/);
    if (nameMatch) {
        name = `${nameMatch[1]} (${parseInt(nameMatch[2]) + 1})`;
    } else {
        name = `${name} (1)`;
    }
    return name;
};

/// #if !BROWSER
// The traffic-light buttons are a native control that does not scale with zoom; when zoomed out,
// compensate --b3-toolbar-left-mac based on zoom to avoid overlapping the toolbar content
export const setToolbarLeftMac = (zoom: number) => {
    // No compensation on non-desktop or non-macOS (let the body--win32 class rules take effect)
    if (!window.siyuan.config || getBackend() !== "darwin") {
        return;
    }
    // The traffic-light buttons are hidden in fullscreen; clear the inline compensation so
    // body--fullscreen's 5px takes effect
    if (zoom >= .9 || document.body.classList.contains("body--fullscreen")) {
        document.body.style.removeProperty("--b3-toolbar-left-mac");
        return;
    }
    // Read the theme's base value from :root (default 74px, for compatibility with third-party
    // themes), and divide by zoom to restore it to the base native pixel value after scaling
    const base = parseInt(getComputedStyle(document.documentElement).getPropertyValue("--b3-toolbar-left-mac")) || 74;
    document.body.style.setProperty("--b3-toolbar-left-mac", (base / zoom * .9) + "px");
};
/// #endif
