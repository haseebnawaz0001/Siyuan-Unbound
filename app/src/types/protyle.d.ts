interface ILuteNode {
    TokensStr: () => string;
    __internal_object__: {
        Parent: {
            Type: number,
        },
        HeadingLevel: string,
    };
}

type THintSource = "search" | "av" | "hint";

type TTurnIntoOne =
    "BlocksMergeSuperBlock"
    | "Blocks2ULs"
    | "Blocks2OLs"
    | "Blocks2TLs"
    | "Blocks2Blockquote"
    | "Blocks2Callout"

type TTurnIntoOneSub = "row" | "col"

type TTurnInto = "Blocks2Ps" | "Blocks2Hs"

type TEditorMode = "preview" | "wysiwyg"

type ILuteRenderCallback = (node: ILuteNode, entering: boolean) => [string, number];

type TProtyleAction = "cb-get-append" | // Append the loaded content, used when scrolling down
    "cb-get-before" | // Prepend the loaded content, used when scrolling up
    "cb-get-unchangeid" | // Scrolling up and down, do not change the block ID while locating
    "cb-get-hl" | // Highlight the target block
    "cb-get-focus" | // Place the cursor in the target block
    "cb-get-focusfirst" | // Dynamically place the cursor in the first block
    "cb-get-setid" | // Clicking an outline item without folding, resets the block ID
    "cb-get-outline" | // Triggered by clicking an item in the outline
    "cb-get-all" | // Load all blocks
    "cb-get-backlink" | // The hover preview is a chained one, so the context has to be shown
    "cb-get-unundo" | // Do not record the change in the undo history
    "cb-get-scroll" | // Scroll to the given position, used when opening a document directly, rootID is required
    "cb-get-search" | // Opened from the search panel
    "cb-get-context" | // Include the surrounding context
    "cb-get-rootscroll" | // Scroll to the given position only if it belongs to rootID, rootID is required
    "cb-get-html" | // Render directly, no extra /api/block/getDocInfo call, otherwise search cannot locate tables
    "cb-get-history" | // Render a document from the history
    "cb-get-opennew" | // A document created while the editor is read-only needs to be temporarily unlocked & https://github.com/siyuan-note/siyuan/issues/12197
    "cb-get-av-no-create"  // Do not create the attribute view automatically

/** @link https://ld246.com/article/1588412297062 */
interface ILuteRender {
    renderDocument?: ILuteRenderCallback;
    renderParagraph?: ILuteRenderCallback;
    renderText?: ILuteRenderCallback;
    renderCodeBlock?: ILuteRenderCallback;
    renderCodeBlockOpenMarker?: ILuteRenderCallback;
    renderCodeBlockInfoMarker?: ILuteRenderCallback;
    renderCodeBlockCode?: ILuteRenderCallback;
    renderCodeBlockCloseMarker?: ILuteRenderCallback;
    renderMathBlock?: ILuteRenderCallback;
    renderMathBlockOpenMarker?: ILuteRenderCallback;
    renderMathBlockContent?: ILuteRenderCallback;
    renderMathBlockCloseMarker?: ILuteRenderCallback;
    renderBlockquote?: ILuteRenderCallback;
    renderBlockquoteMarker?: ILuteRenderCallback;
    renderHeading?: ILuteRenderCallback;
    renderHeadingC8hMarker?: ILuteRenderCallback;
    renderList?: ILuteRenderCallback;
    renderListItem?: ILuteRenderCallback;
    renderTaskListItemMarker?: ILuteRenderCallback;
    renderThematicBreak?: ILuteRenderCallback;
    renderHTML?: ILuteRenderCallback;
    renderTable?: ILuteRenderCallback;
    renderTableHead?: ILuteRenderCallback;
    renderTableRow?: ILuteRenderCallback;
    renderTableCell?: ILuteRenderCallback;
    renderCodeSpan?: ILuteRenderCallback;
    renderCodeSpanOpenMarker?: ILuteRenderCallback;
    renderCodeSpanContent?: ILuteRenderCallback;
    renderCodeSpanCloseMarker?: ILuteRenderCallback;
    renderInlineMath?: ILuteRenderCallback;
    renderInlineMathOpenMarker?: ILuteRenderCallback;
    renderInlineMathContent?: ILuteRenderCallback;
    renderInlineMathCloseMarker?: ILuteRenderCallback;
    renderEmphasis?: ILuteRenderCallback;
    renderEmAsteriskOpenMarker?: ILuteRenderCallback;
    renderEmAsteriskCloseMarker?: ILuteRenderCallback;
    renderEmUnderscoreOpenMarker?: ILuteRenderCallback;
    renderEmUnderscoreCloseMarker?: ILuteRenderCallback;
    renderStrong?: ILuteRenderCallback;
    renderStrongA6kOpenMarker?: ILuteRenderCallback;
    renderStrongA6kCloseMarker?: ILuteRenderCallback;
    renderStrongU8eOpenMarker?: ILuteRenderCallback;
    renderStrongU8eCloseMarker?: ILuteRenderCallback;
    renderStrikethrough?: ILuteRenderCallback;
    renderStrikethrough1OpenMarker?: ILuteRenderCallback;
    renderStrikethrough1CloseMarker?: ILuteRenderCallback;
    renderStrikethrough2OpenMarker?: ILuteRenderCallback;
    renderStrikethrough2CloseMarker?: ILuteRenderCallback;
    renderHardBreak?: ILuteRenderCallback;
    renderSoftBreak?: ILuteRenderCallback;
    renderInlineHTML?: ILuteRenderCallback;
    renderLink?: ILuteRenderCallback;
    renderOpenBracket?: ILuteRenderCallback;
    renderCloseBracket?: ILuteRenderCallback;
    renderOpenParen?: ILuteRenderCallback;
    renderCloseParen?: ILuteRenderCallback;
    renderLinkText?: ILuteRenderCallback;
    renderLinkSpace?: ILuteRenderCallback;
    renderLinkDest?: ILuteRenderCallback;
    renderLinkTitle?: ILuteRenderCallback;
    renderImage?: ILuteRenderCallback;
    renderBang?: ILuteRenderCallback;
    renderEmoji?: ILuteRenderCallback;
    renderEmojiUnicode?: ILuteRenderCallback;
    renderEmojiImg?: ILuteRenderCallback;
    renderEmojiAlias?: ILuteRenderCallback;
    renderBackslash?: ILuteRenderCallback;
    renderBackslashContent?: ILuteRenderCallback;
}

interface IBreadcrumb {
    id: string,
    name: string,
    type: string,
    subType: string,
    children: []
}

interface ILuteOptions extends IMarkdownConfig {
    emojis: IObject;
    emojiSite: string;
    headingAnchor?: boolean;
    lazyLoadImage?: string;
}

declare class Viz {
    public static instance(): Promise<Viz>;

    renderSVGElement: (code: string) => SVGElement;
}

declare class Viewer {
    public destroyed: boolean;

    constructor(element: Element, options: {
        title: [number, (image: HTMLImageElement, imageData: IObject) => string],
        button: boolean,
        initialViewIndex?: number,
        transition: boolean,
        hidden: () => void,
        toolbar: {
            zoomIn: boolean,
            zoomOut: boolean,
            oneToOne: boolean,
            reset: boolean,
            prev: boolean,
            play: boolean,
            next: boolean,
            rotateLeft: boolean,
            rotateRight: boolean,
            flipHorizontal: boolean,
            flipVertical: boolean,
            close: () => void
        }
    })

    public destroy(): void

    public show(): void
}

declare class Lute {
    public static WalkStop: number;
    public static WalkSkipChildren: number;
    public static WalkContinue: number;
    public static Version: string;
    public static Caret: string;

    public static New(): Lute;

    public static EChartsMindmapStr(text: string): string;

    public static NewNodeID(): string;

    public static Sanitize(html: string): string;

    public static EscapeHTMLStr(str: string): string;

    public static UnEscapeHTMLStr(str: string): string;

    public static GetHeadingID(node: ILuteNode): string;

    public static BlockDOM2Content(html: string): string;

    private constructor();

    public BlockDOM2Content(text: string): string;

    public BlockDOM2EscapeMarkerContent(text: string): string;

    public SetSpin(enable: boolean): void;

    public SetTextMark(enable: boolean): void;

    public SetHTMLTag2TextMark(enable: boolean): void;

    public SetHeadingID(enable: boolean): void;

    public SetProtyleMarkNetImg(enable: boolean): void;

    public SetSpellcheck(enable: boolean): void;

    public SetFileAnnotationRef(enable: boolean): void;

    public SetSetext(enable: boolean): void;

    public SetYamlFrontMatter(enable: boolean): void;

    public SetChineseParagraphBeginningSpace(enable: boolean): void;

    public SetRenderListStyle(enable: boolean): void;

    public SetImgPathAllowSpace(enable: boolean): void;

    public SetKramdownIAL(enable: boolean): void;

    public BlockDOM2Md(html: string): string;

    public BlockDOM2StdMd(html: string): string;

    public SetSuperBlock(enable: boolean): void;

    public SetCallout(enable: boolean): void;

    public SetTag(enable: boolean): void;

    public SetInlineMath(enable: boolean): void;

    public SetGFMStrikethrough(enable: boolean): void;

    public SetGFMStrikethrough1(enable: boolean): void;

    public SetMark(enable: boolean): void;

    public SetSub(enable: boolean): void;

    public SetSup(enable: boolean): void;

    public SetInlineAsterisk(enable: boolean): void;

    public SetInlineUnderscore(enable: boolean): void;

    public SetBlockRef(enable: boolean): void;

    public SetSanitize(enable: boolean): void;

    public SetHeadingAnchor(enable: boolean): void;

    public SetImageLazyLoading(imagePath: string): void;

    public SetInlineMathAllowDigitAfterOpenMarker(enable: boolean): void;

    public SetToC(enable: boolean): void;

    public SetIndentCodeBlock(enable: boolean): void;

    public SetParagraphBeginningSpace(enable: boolean): void;

    public SetFootnotes(enable: boolean): void;

    public SetLinkRef(enable: boolean): void;

    public SetEmojiSite(emojiSite: string): void;

    public PutEmojis(emojis: IObject): void;

    public SpinBlockDOM(html: string): string;

    public Md2BlockDOM(html: string): string;

    public Md2BlockDOMWithAutoLink(html: string): string;

    public SetProtyleWYSIWYG(wysiwyg: boolean): void;

    public MarkdownStr(name: string, md: string): string;

    public ProtylePreviewStr(name: string, md: string): string;

    public GetLinkDest(text: string): string;

    public BlockDOM2InlineBlockDOM(html: string): string;

    public BlockDOM2HTML(html: string): string;

    public HTML2Md(html: string): string;

    public HTML2BlockDOM(html: string): string;

    public SetUnorderedListMarker(marker: string): void;

    public SetDataTask(marker: boolean): void;

    public SetExportNormalizeTaskListMarker(marker: boolean): void;

    public SetArbitraryTaskListItemMarker(marker: boolean): void;

    public SetEnsureListItemParagraph(enable: boolean): void;
}

declare const webkitAudioContext: {
    prototype: AudioContext
    new(contextOptions?: AudioContextOptions): AudioContext,
};

/** @link https://ld246.com/article/1549638745630#options-upload */
interface IUpload {
    /** Upload URL */
    url?: string;
    /** Maximum size of an uploaded file, in bytes */
    max?: number;
    /** When the clipboard contains an image address, re-upload the image through this URL */
    linkToImgUrl?: string;
    /** CORS upload validation, sent in the X-Upload-Token header */
    token?: string;
    /** Accepted file types, same as [input accept](https://www.w3schools.com/tags/att_input_accept.asp) */
    accept?: string;
    /** Cross-site access control. Default: false */
    withCredentials?: boolean;
    /** Request headers */
    headers?: Record<string, string>;
    /** Additional request parameters */
    extraData?: { [key: string]: string | Blob };
    /** Name of the upload form field. Default: file[] */
    fieldName?: string;

    /** Called before every upload to build the request headers again */
    setHeaders?(): IObject;

    /** Called after a successful upload */
    success?(editor: HTMLDivElement, msg: string): void;

    /** Called after a failed upload */
    error?(msg: string): void;

    /** Sanitizes the file name. Default: name => name.replace(/\W/g, '') */
    filename?(name: string): string;

    /** Validation, returns true on success, otherwise an error message */
    validate?(files: File[]): string | boolean;

    /** Custom upload implementation, returns an error message if the upload fails */
    handler?(files: File[]): string | null;

    /** Converts the data returned by the server into the built-in data structure */
    format?(files: File[], responseText: string): string;

    /** Processes the files before they are uploaded and returns them  */
    file?(files: File[]): File[];

    /** Called after an image address has been uploaded  */
    linkToImgCallback?(responseText: string): void;
}

interface IScrollAttr {
    rootId: string,
    startId?: string,
    endId?: string
    scrollTop?: number,
    focusId?: string,
    focusStart?: number
    focusEnd?: number
    zoomInId?: string
}

/** @link https://ld246.com/article/1549638745630#options-toolbar */
interface IMenuItem {
    /** Unique identifier */
    name: string;
    /** Tooltip text */
    tip?: string;
    /** Key of the i18n message to use as the label */
    lang?: string;
    /** SVG icon */
    icon?: string;
    /** Keyboard shortcut */
    hotkey?: string;
    /** Position of the tooltip */
    tipPosition?: string;
    /** Whether to show the item in the lite toolbar. Default: false */
    showInLite?: boolean;

    click?(protyle: import("../protyle").Protyle): void;
}

/** @link https://ld246.com/article/1549638745630#options-preview-markdown */
interface IMarkdownConfig {
    /** Whether to indent the beginning of a paragraph by two spaces. Default: false */
    paragraphBeginningSpace?: boolean;
    /** Whether to enable XSS filtering. Default: true */
    sanitize?: boolean;
    /** Marks lists so that they can be [styled individually](https://github.com/Vanessa219/vditor/issues/390) Default: false */
    listStyle?: boolean;
}

/** @link https://ld246.com/article/1549638745630#options-preview */
interface IPreview {
    /** Preview debounce interval in milliseconds. Default: 1000 */
    delay?: number;
    /** Display mode. Default: 'both' */
    mode?: "both" | "editor";
    /** Endpoint used to parse the Markdown */
    url?: string;
    /** @link https://ld246.com/article/1549638745630#options-preview-markdown */
    markdown?: IMarkdownConfig;
    /** @link https://ld246.com/article/1549638745630#options-preview-actions  */
    actions?: Array<IPreviewAction | IPreviewActionCustom>;

    /** Called before the preview is rendered */
    transform?(html: string): string;
}

type IPreviewAction = "desktop" | "tablet" | "mobile" | "mp-wechat" | "zhihu" | "yuque";

interface IPreviewActionCustom {
    /** Key of the action */
    key: string;
    /** Button label */
    text: string;
    /** Value of the button className */
    className?: string;
    /** Called when the button is clicked */
    click: (key: string) => void;
}

interface IHintData {
    id?: string;
    html: string;
    value: string;
    filter?: string[];
    focus?: boolean;
}

interface IHintExtend {
    key: string;

    hint?(value: string, protyle: IProtyle, source: THintSource): IHintData[];
}

/** @link https://ld246.com/article/1549638745630#options-hint */
interface IHint {
    /** HTML appended to the frequently used emoji hint */
    emojiTail?: string;
    /** Hint debounce interval in milliseconds. Default: 200 */
    delay?: number;
    /** Default emojis, either picked from [lute/emoji_map](https://github.com/88250/lute/blob/master/parse/emoji_map.go#L32) or custom ones */
    emoji?: IObject;
    emojiPath?: string;
    extend?: IHintExtend[];
}

/** @link https://ld246.com/article/1549638745630#options */
interface IProtyleOptions {
    databaseAttr?: boolean,
    history?: {
        created?: string
        snapshot?: string
    },
    backlinkData?: {
        blockPaths: IBreadcrumb[],
        dom: string
        expand: boolean
    }[],
    action?: TProtyleAction[],
    scrollPosition?: ScrollLogicalPosition,
    mode?: TEditorMode,
    blockId?: string
    rootId?: string
    notebookId?: string
    originalRefBlockIDs?: IObject
    key?: string
    defIds?: string[]
    render?: {
        background?: boolean
        title?: boolean
        titleShowTop?: boolean
        gutter?: boolean
        scroll?: boolean
        breadcrumb?: boolean
        breadcrumbDocName?: boolean
        hideTitleOnZoom?: boolean
    }
    /** For internal debugging only */
    _lutePath?: string;
    /** Whether to enable typewriter mode. Default: false */
    typewriterMode?: boolean;
    toolbar?: Array<string | IMenuItem>;
    /** @link https://ld246.com/article/1549638745630#options-preview */
    preview?: IPreview;
    /** @link https://ld246.com/article/1549638745630#options-hint */
    hint?: IHint;
    /** @link https://ld246.com/article/1549638745630#options-upload */
    upload?: IUpload;
    /** @link https://ld246.com/article/1549638745630#options-classes */
    classes?: {
        preview?: string;
    };
    click?: {
        /** Whether clicking below the last block is prevented from inserting a new block */
        preventInsetEmptyBlock?: boolean
    }

    handleEmptyContent?(): void

    /** Called once the editor has finished its asynchronous rendering */
    after?(protyle: import("../protyle").Protyle): void;

    /** Whether to use the lite version of the editor */
    lite?: boolean;
}

interface IProtyle {
    highlight: {
        mark: Highlight
        markHL: Highlight
        ranges: Range[]
        rangeIndex: number
        styleElement: HTMLStyleElement
    }
    getInstance: () => import("../protyle").Protyle,
    observerLoad?: ResizeObserver,
    observer?: ResizeObserver,
    app: import("../index").App,
    id: string,
    query?: {
        key: string,
        method: number
        types: Config.IUILayoutTabSearchConfigTypes
        subTypes: Config.IUILayoutTabSearchConfigSubTypes
    },
    block: {
        id?: string,
        scroll?: boolean
        parentID?: string,
        parent2ID?: string,
        rootID?: string,
        showAll?: boolean
        mode?: number
        blockCount?: number
        action?: TProtyleAction[]
    },
    disabled: boolean,
    lite?: boolean,
    selectElement?: HTMLElement,
    ws?: import("../layout/Model").Model,
    notebookId?: string
    path?: string
    model?: import("../../src/editor").Editor,
    updated: boolean;
    element: HTMLElement;
    scroll?: import("../protyle/scroll").Scroll,
    gutter?: import("../protyle/gutter").Gutter,
    breadcrumb?: import("../protyle/breadcrumb").Breadcrumb,
    title?: import("../protyle/header/Title").Title,
    background?: import("../protyle/header/background").Background,
    databaseAttributePanel?: import("../protyle/render/av/attributePanel").AVAttributePanel,
    contentElement?: HTMLElement,
    options: IProtyleOptions;
    lute?: Lute;
    toolbar?: import("../protyle/toolbar").Toolbar,
    preview?: import("../protyle/preview").Preview;
    hint?: import("../protyle/hint").Hint;
    upload?: import("../protyle/upload").Upload;
    undo?: import("../protyle/undo").IUndo;
    wysiwyg?: import("../protyle/wysiwyg").WYSIWYG
}
