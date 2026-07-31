import {hasClosestByAttribute, hasClosestByClassName} from "../../protyle/util/hasClosest";

export let keyboardLockUntil = 0;

export const armKeyboardLock = () => {
    // On some devices (e.g. HarmonyOS Pura X), showing the keyboard immediately triggers activeBlur, which closes
    // the keyboard again; some mobile browsers (e.g. Samsung Keyboard) also trigger resize once the editor gains
    // focus, which likewise closes the keyboard right away. So whenever we actively show the keyboard or tap an
    // editable area, lock out activeBlur-triggered keyboard closing for a short period.
    keyboardLockUntil = Date.now() + 500;
};

export const callMobileAppShowKeyboard = () => {
    armKeyboardLock();

    if (window.JSAndroid && window.JSAndroid.showKeyboard) {
        window.JSAndroid.showKeyboard();
    } else if (window.JSHarmony && window.JSHarmony.showKeyboard) {
        window.JSHarmony.showKeyboard();
    }
};


export const canInput = (element: Element) => {
    if (!element || element.nodeType !== 1) {
        return false;
    }
    if ((
        element.tagName === "TEXTAREA" ||
        (element.tagName === "INPUT" && ["email", "number", "password", "search", "tel", "text", "url", "", null].includes(element.getAttribute("type")))
    ) && element.getAttribute("readonly") !== "readonly") {
        return element;
    }
    const wysiwygElement = hasClosestByClassName(element, "protyle-wysiwyg", true);
    if (wysiwygElement && wysiwygElement.getAttribute("data-readonly") === "false") {
        return hasClosestByAttribute(element, "contenteditable", "true");
    }
    return false;
};

export const setWebViewFocusable = () => {
    if ((window.JSAndroid || window.JSHarmony) && document.activeElement.tagName === "IFRAME") {
        if (window.JSAndroid?.setWebViewFocusable) {
            window.JSAndroid.setWebViewFocusable(true);
        } else if (window.JSHarmony?.setWebViewFocusable) {
            window.JSHarmony.setWebViewFocusable(true);
        }
    }
};
