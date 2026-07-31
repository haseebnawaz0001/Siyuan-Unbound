/** Title of a setting item */
export const genConfigItemName = (title: string): string =>
    `<div class="config-name">${title}</div>`;

/** Left hand area of a setting item (title and description) */
export const genConfigItemMainHtml = (title: string, desc?: string): string =>
    `<div class="fn__flex-1">
    ${genConfigItemName(title)}
    ${desc ? `<div class="b3-label__text">${desc}</div>` : ""}
</div>`;

const genSwitchInputHtml = (id: string, checked: boolean): string =>
    `<input class="b3-switch fn__flex-center" id="${id}" type="checkbox"${checked ? " checked" : ""}/>`;

export const genSwitchRow = (id: string, title: string, desc: string | undefined, checked: boolean): string =>
    `<label class="fn__flex b3-label config-item">
    ${genConfigItemMainHtml(title, desc)}
    <span class="fn__space"></span>
    ${genSwitchInputHtml(id, checked)}
</label>`;

/** Switch rendered as a list item (`b3-list-item` layout) */
export const genListSwitchItemHtml = (id: string, label: string, checked: boolean): string =>
    `<label class="b3-list-item">
    <div class="fn__flex-1 ft__on-surface">${label}</div>
    <span class="fn__space"></span>
    ${genSwitchInputHtml(id, checked)}
</label>`;

/** Binds the show / hide icon of the password box with the given `id` (matches the DOM the `input-password` branch of `textBlock` renders) */
export const bindPasswordIconaToggle = (root: HTMLElement, inputId: string): void => {
    root.querySelector<HTMLElement>(`#${CSS.escape(inputId)} + .b3-form__icona-icon[data-action="togglePassword"]`)?.addEventListener("click", (event) => {
        const svg = event.currentTarget as SVGSVGElement;
        const icon = svg.firstElementChild as SVGUseElement;
        const field = svg.previousElementSibling as HTMLInputElement;
        if (!icon || !field) {
            return;
        }
        const isEye = icon.getAttribute("xlink:href") === "#iconEye";
        icon.setAttribute("xlink:href", isEye ? "#iconEyeoff" : "#iconEye");
        field.setAttribute("type", isEye ? "text" : "password");
    });
};
