import {addScript} from "../util/addScript";
import {addStyle} from "../util/addStyle";
import {Constants} from "../../constants";
import {hasNextSibling, hasPreviousSibling} from "../wysiwyg/getBlock";
import {hasClosestBlock} from "../util/hasClosest";
import {looseJsonParse} from "../../util/functions";
import {genRenderFrame} from "./util";

export const mathRender = (element: Element, cdn = Constants.PROTYLE_CDN, maxWidth = false) => {
    let mathElements: Element[] | NodeListOf<Element> = [];
    if (element.getAttribute("data-subtype") === "math" && element.getAttribute("data-render") !== "true") {
        mathElements = [element];
    } else {
        mathElements = element.querySelectorAll('[data-subtype="math"]:not([data-render="true"])');
    }
    if (mathElements.length === 0) {
        return;
    }
    addStyle(`${cdn}/js/katex/katex.min.css?v=0.16.9`, "protyleKatexStyle");
    addScript(`${cdn}/js/katex/katex.min.js?v=0.16.9`, "protyleKatexScript").then(() => {
        addScript(`${cdn}/js/katex/mhchem.min.js?v=0.16.9`, "protyleKatexMhchemScript").then(() => {
            mathElements.forEach((mathElement: HTMLElement) => {
                mathElement.setAttribute("data-render", "true");
                let macros = {};
                try {
                    macros = looseJsonParse(window.siyuan.config.editor.katexMacros || "{}");
                } catch (e) {
                    console.warn("KaTex macros is not JSON", e);
                }
                const isBlock = mathElement.tagName === "DIV";
                try {
                    const mathHTML = window.katex.renderToString(Lute.UnEscapeHTMLStr(mathElement.getAttribute("data-content")), {
                        displayMode: isBlock,
                        output: "html",
                        macros,
                        trust: true, // REF: https://katex.org/docs/supported#html
                        strict: (errorCode) => errorCode === "unicodeTextInMathMode" ? "ignore" : "warn",
                    });
                    const blockElement = hasClosestBlock(mathElement);
                    if (isBlock) {
                        genRenderFrame(mathElement);
                        mathElement.firstElementChild.firstElementChild.classList.remove("ft__error");
                        mathElement.firstElementChild.firstElementChild.setAttribute("contenteditable", "false");
                        mathElement.firstElementChild.firstElementChild.innerHTML = mathHTML;
                        // https://github.com/siyuan-note/siyuan/issues/3541
                        const baseElements = mathElement.querySelectorAll(".base");
                        if (baseElements.length > 0) {
                            baseElements[baseElements.length - 1].insertAdjacentHTML("afterend", "<span class='fn__flex-1'></span>");
                        }
                        // https://github.com/siyuan-note/siyuan/issues/4334
                        const newlineElement = mathElement.querySelector(".katex-html > .newline");
                        if (newlineElement) {
                            newlineElement.parentElement.style.display = "block";
                        }
                    } else {
                        mathElement.classList.remove("ft__error");
                        mathElement.innerHTML = mathHTML;
                        if (blockElement && mathElement.getBoundingClientRect().width > blockElement.clientWidth) {
                            mathElement.style.maxWidth = "100%";
                            mathElement.style.overflowX = "auto";
                            mathElement.style.overflowY = "hidden";
                            mathElement.style.display = "inline-block";
                        } else {
                            mathElement.style.maxWidth = "";
                            mathElement.style.overflowX = "";
                            mathElement.style.overflowY = "";
                            mathElement.style.display = "";
                        }
                        const nextSibling = hasNextSibling(mathElement) as HTMLElement;
                        if (!nextSibling) {
                            // Table editing issue https://ld246.com/article/1629191424824
                            if (mathElement.parentElement.tagName !== "TH" && mathElement.parentElement.tagName !== "TD") {
                                // Cursor cannot be moved to the end https://github.com/siyuan-note/siyuan/issues/2112
                                mathElement.insertAdjacentText("afterend", "\n");
                            } else {
                                // https://ld246.com/article/1651595975481, https://ld246.com/article/1658903123429
                                // Changed from beforeend to afterend as browsers have been upgraded
                                mathElement.insertAdjacentText("afterend", Constants.ZWSP);
                            }
                        } else if (nextSibling && nextSibling.nodeType !== 3 &&
                            (
                                nextSibling.getAttribute("data-type")?.indexOf("inline-math") > -1 ||
                                nextSibling.classList.contains("img")
                            )) {
                            // Deleting an adjacent math formula or moving the cursor has issues
                            mathElement.after(document.createTextNode(Constants.ZWSP));
                        } else if (nextSibling &&
                            !nextSibling.textContent.startsWith("\n") && // https://github.com/siyuan-note/insider/issues/1089
                            // After typing $a$, moving the cursor to another block, then clicking on a, the cursor doesn't show https://github.com/siyuan-note/insider/issues/1076#issuecomment-1253215515
                            nextSibling.textContent !== Constants.ZWSP) {
                            // Deleting the character right after a math formula removes an extra br https://ld246.com/article/1647157880974
                            // Cannot add &#xFEFF; again when there's already a \n after a math formula https://ld246.com/article/1647329437541
                            mathElement.insertAdjacentHTML("beforeend", "&#xFEFF;");
                        }
                        // Cursor cannot be moved to the start of the paragraph https://ld246.com/article/1623551823742
                        if (mathElement.previousSibling?.textContent.endsWith("\n")) {
                            mathElement.insertAdjacentText("beforebegin", Constants.ZWSP);
                        } else if (!hasPreviousSibling(mathElement) && ["TH", "TD"].includes(mathElement.parentElement.tagName)) {
                            // When a cell contains only a math formula, the cursor cannot be moved before the math formula
                            mathElement.insertAdjacentText("afterbegin", Constants.ZWSP);
                        }
                    }

                    // export pdf
                    if (maxWidth) {
                        setTimeout(() => {
                            if (isBlock) {
                                const katexElement = mathElement.querySelector(".katex-display");
                                if (katexElement.clientWidth < katexElement.scrollWidth) {
                                    katexElement.firstElementChild.setAttribute("style", `font-size:${katexElement.clientWidth * 100 / katexElement.scrollWidth}%`);
                                }
                            } else {
                                if (blockElement && mathElement.offsetWidth > blockElement.clientWidth) {
                                    mathElement.firstElementChild.setAttribute("style", `font-size:${blockElement.clientWidth * 100 / mathElement.offsetWidth}%`);
                                }
                            }
                        });
                    }
                } catch (e) {
                    if (isBlock) {
                        genRenderFrame(mathElement);
                        mathElement.firstElementChild.firstElementChild.setAttribute("contenteditable", "false");
                        mathElement.firstElementChild.firstElementChild.innerHTML = e.message;
                        mathElement.firstElementChild.firstElementChild.classList.add("ft__error");
                    } else {
                        mathElement.innerHTML = e.message;
                        mathElement.classList.add("ft__error");
                    }
                }
            });
        });
    });
};
