export const img3115 = (imgElement: HTMLElement) => {
    // Remove the pre-3.1.15 .img width style
    if (imgElement.style.minWidth) {
        // Centering needs the minWidth style, so the style attribute can't be removed
        imgElement.style.width = "";
    } else {
        imgElement.removeAttribute("style");
    }
};
