import type {SettingControl} from "../setting/control";

/** Composite row: text and control parts, rendered and indexed for search by the engine */
export type RowPart =
    | {
        kind: "title";
        text: string;
    }
    | {
        kind: "desc";
        text: string;
    }
    | SettingControl;

export const isSettingControl = (part: RowPart): part is SettingControl =>
    "readConfig" in part && "readValue" in part;

/** A single switch inside a `config-query` grid */
type SwitchQuerySwitchItem = Extract<SettingControl, {kind: "switch"}> & {
    label: string;
    icon?: string;
};

/** A single number box inside a `config-query` grid */
type SwitchQueryNumberItem = Extract<SettingControl, {kind: "number"}> & {
    label: string;
};

export type SwitchQueryItem = SwitchQuerySwitchItem | SwitchQueryNumberItem;

/** Left column of a stack row */
export type StackLeft =
    | {kind: "title"; text: string}
    | {kind: "desc"; text: string}
    | Extract<SettingControl, {kind: "textBlock"}>;

/** Control in the right column of a stack row */
export type StackRight =
    | {kind: "button"; id: string; label: string; icon: string}
    | Extract<SettingControl, {kind: "switch" | "number" | "select"}>;

export type StackLine = {
    left: StackLeft;
    right?: StackRight;
};
