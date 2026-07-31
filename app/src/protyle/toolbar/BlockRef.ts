import {ToolbarItem} from "./ToolbarItem";
import {hintRef} from "../hint/extend";
import {fixTableRange} from "../util/selection";

export class BlockRef extends ToolbarItem {
    public element: HTMLElement;

    constructor(protyle: IProtyle, menuItem: IMenuItem) {
        super(protyle, menuItem);
        // Cannot use getEventName, otherwise the cursor position would shift to the clicked document
        this.element.addEventListener("click", (event: MouseEvent & { changedTouches: MouseEvent[] }) => {
            if (protyle.toolbar.range.toString() === "") {
                return;
            }
            fixTableRange(protyle.toolbar.range);
            hintRef(protyle.toolbar.range.toString(), protyle, "search");
            protyle.toolbar.element.classList.add("fn__none");
            event.stopPropagation();
        });
    }
}
