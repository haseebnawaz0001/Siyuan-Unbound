type TPluginDockPosition = "LeftTop" | "LeftBottom" | "RightTop" | "RightBottom" | "BottomLeft" | "BottomRight"
type TDockPosition = "Left" | "Right" | "Bottom"
type TWS = "main" | "filetree" | "protyle" | "backlink" | "bookmark" | "graph" | "outline" | "tag" | "agentChat"
type TDock = "file" | "outline" | "inbox" | "bookmark" | "tag" | "graph" | "globalGraph" | "backlink" | "agentChat"
type TTab = "Outline" | "Graph" | "Backlink" | "Asset" | "Editor" | "Search" | "siyuan-card"
type TOperation =
    "insert"
    | "restoreCreatedDoc"
    | "removeCreatedDoc"
    | "update"
    | "delete"
    | "move"
    | "foldHeading"
    | "unfoldHeading"
    | "setAttrs"
    | "updateAttrs"
    | "append"
    | "insertAttrViewBlock"
    | "removeAttrViewBlock"
    | "addAttrViewCol"
    | "removeAttrViewCol"
    | "addFlashcards"
    | "removeFlashcards"
    | "updateAttrViewCell"
    | "updateAttrViewCol"
    | "updateAttrViewColTemplate"
    | "sortAttrViewRow"
    | "sortAttrViewCol"
    | "sortAttrViewKey"
    | "setAttrViewColPin"
    | "setAttrViewColHidden"
    | "setAttrViewColWrap"
    | "setAttrViewColWidth"
    | "setAttrViewColAlign"
    | "updateAttrViewColOptions"
    | "removeAttrViewColOption"
    | "updateAttrViewColOption"
    | "setAttrViewName"
    | "setAttrViewNewItemTemplates"
    | "doUpdateUpdated"
    | "duplicateAttrViewKey"
    | "setAttrViewColIcon"
    | "setAttrViewFilters"
    | "setAttrViewSorts"
    | "setAttrViewColCalc"
    | "updateAttrViewColNumberFormat"
    | "replaceAttrViewBlock"
    | "addAttrViewView"
    | "setAttrViewViewName"
    | "removeAttrViewView"
    | "setAttrViewViewIcon"
    | "duplicateAttrViewView"
    | "duplicateAttrViewRow"
    | "sortAttrViewView"
    | "setAttrViewPageSize"
    | "updateAttrViewColRelation"
    | "moveOutlineHeading"
    | "updateAttrViewColRollup"
    | "hideAttrViewName"
    | "setAttrViewCardSize"
    | "setAttrViewCardAspectRatio"
    | "setAttrViewCoverFrom"
    | "setAttrViewCoverFromAssetKeyID"
    | "setAttrViewFitImage"
    | "setAttrViewShowIcon"
    | "setAttrViewWrapField"
    | "setAttrViewColDateFillCreated"
    | "setAttrViewColDateFillSpecificTime"
    | "setAttrViewViewDesc"
    | "setAttrViewColDesc"
    | "setAttrViewBlockView"
    | "setAttrViewGroup"
    | "removeAttrViewGroup"
    | "hideAttrViewAllGroups"
    | "syncAttrViewTableColWidth"
    | "hideAttrViewGroup"
    | "sortAttrViewGroup"
    | "foldAttrViewGroup"
    | "setAttrViewDisplayFieldName"
    | "setAttrViewFillColBackgroundColor"
    | "setAttrViewUpdatedIncludeTime"
    | "setAttrViewCreatedIncludeTime"
type TBazaarType = "templates" | "icons" | "widgets" | "themes" | "plugins"
type TCardType = "doc" | "notebook" | "all"
type TEventBus = "ws-main" | "sync-start" | "sync-end" | "sync-fail" |
    "click-blockicon" | "click-editorcontent" | "click-pdf" | "click-editortitleicon" | "click-flashcard-action" |
    "open-noneditableblock" |
    "open-menu-blockref" | "open-menu-fileannotationref" | "open-menu-tag" | "open-menu-link" | "open-menu-image" |
    "open-menu-av" | "open-menu-content" | "open-menu-breadcrumbmore" | "open-menu-doctree" | "open-menu-inbox" |
    "open-siyuan-url-plugin" | "open-siyuan-url-block" | "opened-notebook" |
    "closed-notebook" |
    "paste" |
    "input-search" |
    "loaded-protyle-dynamic" | "loaded-protyle-static" |
    "switch-protyle" | "switch-protyle-mode" |
    "destroy-protyle" |
    "lock-screen" |
    "mobile-keyboard-show" | "mobile-keyboard-hide" |
    "code-language-update" | "code-language-change" |
    "kernel-plugin-state-change"
type TAVView = "table" | "gallery" | "kanban"
type TAVAlign = "" | "left" | "center" | "right"
type TAVCol =
    "text"
    | "date"
    | "number"
    | "relation"
    | "rollup"
    | "select"
    | "block"
    | "mSelect"
    | "url"
    | "email"
    | "phone"
    | "mAsset"
    | "template"
    | "created"
    | "updated"
    | "checkbox"
    | "lineNumber"
type TAVFilterOperator =
    "="
    | "!="
    | ">"
    | ">="
    | "<"
    | "<="
    | "Contains"
    | "Does not contains"
    | "Is empty"
    | "Is not empty"
    | "Starts with"
    | "Ends with"
    | "Is between"
    | "Is relative to today"
    | "Is true"
    | "Is false"

type TRecentDocsSort = "viewedAt" | "closedAt" | "openAt" | "updated"
type TPublishAccessLevel = "public" | "protected" | "hidden" | "private" | "forbidden";

/**
 * Kernel plugin state
 * - `-1`: inactive, the kernel plugin is not installed or not available
 * - `0`: ready, the kernel plugin is installed but has not been started
 * - `1`: loading, the kernel plugin is starting up
 * - `2`: running, the kernel plugin is running and ready to use
 * - `3`: stopping, the kernel plugin is shutting down
 * - `4`: stopped, the kernel plugin has stopped
 * - `5`: error, the kernel plugin hit an unrecoverable error
 */
type TKernelPluginState = -1 | 0 | 1 | 2 | 3 | 4 | 5

type TJsonRpcId = string | number;
type TJsonRpcMethod = string;
type TJsonRpcPositionalParams = any[];
type TJsonRpcNamedParams = Record<string, any>;
type TJsonRpcParams = TJsonRpcPositionalParams | TJsonRpcNamedParams | undefined;
type TJsonRpcMethodParams = TJsonRpcPositionalParams | [TJsonRpcNamedParams] | [];
type TJsonRpcHandler<T = any> = (...args: TJsonRpcMethodParams) => Promise<T> | T;

declare module "blueimp-md5"

declare class Highlight {
    constructor(...range: Range[]);

    add(range: Range): void

    clear(): void

    forEach(callbackfn: (value: Range, key: number) => void): void;
}

declare namespace CSS {
    const highlights: Map<string, Highlight>;
}

interface CSSStyleDeclarationElectron extends CSSStyleDeclaration {
    WebkitAppRegion: string;
}

interface Window {
    DOMPurify: {
        sanitize(dirty: string, options?: any): string;
    };
    echarts: {
        init(element: Element, theme?: string, options?: {
            width: number
        }): {
            setOption(option: any): void;
            getZr(): any;
            on(name: string, event: (e: any) => void): any;
            containPixel(name: string, position: number[]): any;
            resize(): void;
        };
        dispose(element: Element): void;
        getInstanceById(id: string): {
            resize: () => void
            clear: () => void
            getOption: () => { series: { type: string }[] }
        };
    };
    ABCJS: {
        renderAbc(element: Element, text: string, options: {
            responsive: string
        }): void;
    };
    MathJax: {
        svg: {
            fontCache: string
        }
        startup?: {
            promise: Promise<void>
        }
        tex2svg?(math: string, options: { display: boolean }): HTMLElement
    };
    hljs: {
        listLanguages(): string[];
        highlight(text: string, options: {
            language?: string,
            ignoreIllegals: boolean
        }): {
            value: string
        };
        getLanguage(text: string): {
            name: string
        };
    };
    katex: {
        renderToString(math: string, option: {
            displayMode: boolean;
            output: string;
            macros: IObject;
            trust: boolean;
            strict: (errorCode: string) => "ignore" | "warn";
        }): string;
    };
    zenuml: object,
    mermaid: {
        initialize(options: any): void,
        render(id: string, text: string): { svg: string },
        registerExternalDiagrams(ex: object[]): void,
        registerIconPacks(options: {
            name: string,
            loader(): Promise<Response>
        }[]): void
    };
    plantumlEncoder: {
        encode(options: string): string,
    };
    pdfjsLib: any;
    webkit: {
        nativeCallbacks: { [key: string]: (id: number) => void },
        messageHandlers: {
            saveExportFile: { postMessage: (url: string) => void }
            openLink: { postMessage: (url: string) => void }
            startKernelFast: { postMessage: (url: string) => void }
            changeStatusBar: { postMessage: (url: string) => void }
            setClipboard: { postMessage: (url: string) => void }
            purchase: { postMessage: (url: string) => void }
            print: { postMessage: (html: string) => void }
            exit: { postMessage: (text: string) => void }
            sendNotification: {
                postMessage: (options: {
                    title: string,
                    body: string,
                    delay: number,
                    callback: string
                }) => number
            }
            cancelNotification: { postMessage: (id: number) => void }
        }
    };
    htmlToImage: {
        toCanvas: (element: Element, options?: IHtmlToImageOptions) => Promise<HTMLCanvasElement>
        toBlob: (element: Element, options?: IHtmlToImageOptions) => Promise<Blob>
    };
    siyuan: ISiyuan;
    JSAndroid: {
        returnDesktop(): void
        openExternal(url: string): void
        exportByDefault(url: string): void
        saveExportFile(url: string): void
        changeStatusBarColor(color: string, mode: number): void
        writeClipboard(text: string): void
        writeHTMLClipboard(text: string, html: string): void
        writeSiYuanHTMLClipboard(text: string, html: string, siyuanHTML: string): void
        writeImageClipboard(uri: string): void
        readClipboard(): string
        readHTMLClipboard(): string
        readSiYuanHTMLClipboard(): string
        getBlockURL(): string
        hideKeyboard(): void
        showKeyboard(): void
        print(title: string, html: string): void
        getScreenWidthPx(): number
        exit(): void
        setWebViewFocusable(enable: boolean): void
        sendNotification(channel: string, title: string, body: string, delayInSeconds: number): number
        cancelNotification(id: number): void
    };
    JSHarmony: {
        showKeyboard(): void
        hideKeyboard(): void
        openExternal(url: string): void
        exportByDefault(url: string): void
        saveExportFile(url: string): void
        changeStatusBarColor(color: string, mode: number): void
        writeClipboard(text: string): void
        writeHTMLClipboard(text: string, html: string): void
        writeSiYuanHTMLClipboard(text: string, html: string, siyuanHTML: string): void
        readClipboard(): string
        readHTMLClipboard(): string
        readSiYuanHTMLClipboard(): string
        returnDesktop(): void
        print(title: string, html: string): void
        getScreenWidthPx(): number
        exit(): void
        setWebViewFocusable(enable: boolean): void
        sendNotification(channel: string, title: string, body: string, delayInSeconds: number): number
        cancelNotification(id: number): void
    };

    Protyle: import("../protyle/method").default;

    lockscreenByMode(): void;

    goBack(): void;

    showMessage(message: string, timeout: number, type: string, messageId?: string): void;

    reconnectWebSocket(): void;

    showKeyboardToolbar(): void;

    processIOSPurchaseResponse(code: number): void;

    hideKeyboardToolbar(): void;

    openFileByURL(URL: string): boolean;

    destroyTheme(): Promise<void>;
}

interface ILocalFiles {
    path: string,
    size: number
}

interface IClipboardData {
    textHTML?: string,
    textPlain?: string,
    siyuanHTML?: string,
    files?: File[],
    localFiles?: ILocalFiles[],
}

interface IRefDefs {
    refID: string,
    defIDs?: string[],
    avItemID?: string,
    avViewID?: string,
    avGroupID?: string,
}

interface IFilesPath {
    notebookId: string,
    openPaths: string[]
}

interface IPosition {
    x: number,
    y: number,
    w?: number,
    h?: number,
    isLeft?: boolean
}

interface ISaveLayout {
    name: string,
    layout: IObject
    time: number
    filesPaths: IFilesPath[]
}

interface IWorkspace {
    path: string;
    closed: boolean;
}

interface ICardPackage {
    id: string;
    updated: string;
    name: string;
    size: number;
}

interface ICard {
    deckID: string;
    cardID: string;
    blockID: string;
    nextDues: Record<string, string>;
    lapses: number;  // Number of times the card was forgotten
    lastReview: number;  // Timestamp of the last review
    reps: number;  // Number of times the card was reviewed
    state: number;   // Card state, 0: new card
}

interface ICardData {
    cards: ICard[],
    unreviewedCount: number
    unreviewedNewCardCount: number
    unreviewedOldCardCount: number
}

interface IPluginSettingOption {
    title: string;
    description?: string;
    actionElement?: HTMLElement;
    direction?: "column" | "row";

    createActionElement?(): HTMLElement;
}

interface ISearchAssetOption {
    keys: string[],
    col: string,
    row: string,
    layout: number,
    method: number,
    types: {
        ".txt": boolean,
        ".md": boolean,
        ".docx": boolean,
        ".xlsx": boolean,
        ".pptx": boolean,
    },
    sort: number,
    k: string,
}

interface ITextOption {
    color?: string,
    type: string
}

interface ISnippet {
    id?: string;
    name: string;
    type: string;
    enabled: boolean;
    content: string;
    disabledInPublish: boolean;
}

interface IInbox {
    oId: string;
    shorthandContent: string;
    shorthandMd: string;
    shorthandDesc: string;
    shorthandFrom: number;
    shorthandTitle: string;
    shorthandURL: string;
    hCreated: string;
}

interface IPdfAnno {
    pages?: {
        index: number
        positions: number[]
    }[]
    index?: number,
    color: string,
    type: string,   // border, text
    content: string,    // rect, text
    mode: string,
    id?: string,
    coords?: number[]
    ids?: string[]
}

interface IBackStack {
    id: string,
    // Mobile only
    data?: {
        startId: string,
        endId: string
        path: string
        notebookId: string
    },
    scrollTop?: number,
    callback?: TProtyleAction[],
    position?: {
        start: number,
        end: number
    }
    // Desktop only
    protyle?: IProtyle,
    zoomId?: string
}

interface IEmojiItem {
    unicode: string,
    description: string,
    description_zh_cn: string,
    description_ja_jp: string,
    keywords: string
}

interface IEmoji {
    id: string,
    title: string,
    title_zh_cn: string,
    title_ja_jp: string,
    items: IEmojiItem[]
}

interface INotebook {
    name: string;
    id: string;
    closed: boolean;
    icon: string;
    sort: number;
    subFileCount: number;
    dueFlashcardCount?: string;
    newFlashcardCount?: string;
    flashcardCount?: string;
    sortMode: number;
    encrypted?: boolean;
}

interface ISiyuan {
    zIndex: number
    storage?: {
        [key: string]: any
    },
    closedTabs?: ILayoutJSON[]
    reqIds: {
        [key: string]: number
    },
    editorIsFullscreen?: boolean,
    hideBreadcrumb?: boolean,
    notebooks?: INotebook[],
    emojis?: IEmoji[],
    backStack?: IBackStack[],
    mobile?: {
        touchRange?: Range
        size: {
            isLandscape?: boolean,
            landscape?: {
                height1: number,
                height2: number,    // Height while the soft keyboard is shown
            }, // Landscape
            portrait?: {
                height1: number,
                height2: number,
            }
        }
        editor?: import("../protyle").Protyle
        popEditor?: import("../protyle").Protyle
        docks?: {
            outline: import("../mobile/dock/MobileOutline").MobileOutline | null,
            file: import("../mobile/dock/MobileFiles").MobileFiles | null,
            bookmark: import("../mobile/dock/MobileBookmarks").MobileBookmarks | null,
            tag: import("../mobile/dock/MobileTags").MobileTags | null,
            backlink: import("../mobile/dock/MobileBacklinks").MobileBacklinks | null,
            inbox: import("../layout/dock/Inbox").Inbox | null,
        } & { [key: string]: import("../layout/Model").Model | any };
    },
    user?: {
        userId: string
        userName: string
        userAvatarURL: string
        userHomeBImgURL: string
        userIntro: string
        userNickname: string
        /**
         * One-time purchase status of the paid features
         * 0: not paid, 1: paid
         */
        userSiYuanOneTimePayStatus: number
        /**
         * Membership expiration time
         * -1: lifetime member; 0: not subscribed or expired; >0: expiration timestamp in milliseconds
         */
        userSiYuanProExpireTime: number
        /**
         * Subscription plan
         * 0: annual subscription or lifetime; 1: education discount; 2: trial
         */
        userSiYuanSubscriptionPlan: number
        /**
         * Subscription type
         * 0: annual; 1: lifetime; 2: monthly
         */
        userSiYuanSubscriptionType: number
        /**
         * Subscription status
         * -1: not subscribed, 0: active, 1: banned, 2: expired (both paid and trial subscriptions)
         */
        userSiYuanSubscriptionStatus: number
        userToken: string
        userTitles: {
            name: string,
            icon: string,
            desc: string
        }[]
    },
    dragElement?: HTMLElement,
    dragTitle?: string,
    currentDragOverTabHeadersElement?: HTMLElement
    touchDragActive?: boolean,
    touchDragGhost?: HTMLElement | null,
    layout?: {
        layout?: import("../layout").Layout,
        centerLayout?: import("../layout").Layout,
        leftDock?: import("../layout/dock").Dock,
        rightDock?: import("../layout/dock").Dock,
        bottomDock?: import("../layout/dock").Dock,
    }
    config?: Config.IConf;
    ws: import("../layout/Model").Model,
    ctrlIsPressed?: boolean,
    altIsPressed?: boolean,
    shiftIsPressed?: boolean,
    coordinates?: {
        pageX: number,
        pageY: number,
        clientX: number,
        clientY: number,
        screenX: number,
        screenY: number,
    },
    menus?: import("../menus").Menus
    languages?: {
        [key: string]: any;
    }
    bookmarkLabel?: string[]
    blockPanels: import("../block/Panel").BlockPanel[],
    dialogs: import("../dialog").Dialog[],
    viewer?: Viewer,
    /**
     * Whether the app is being accessed through the publish service
     */
    isPublish?: boolean;
}

interface IOperation {
    action: TOperation, // move and delete do not need to pass data
    id?: string,
    context?: Record<string, string>,  // focusId, message, ignoreProcess, setRange
    blockID?: string,
    isTwoWay?: boolean, // Whether the relation is two-way
    backRelationKeyID?: string, // ID of the target relation column of the two-way relation
    avID?: string,  // av
    format?: string // Only used by updateAttrViewColNumberFormat
    keyID?: string // Only used by updateAttrViewCell
    rowID?: string // Only used by updateAttrViewCell
    data?: any, // updateAttr: { old: IObject, new: IObject }; updateAttrViewCell: {TAVCol: {content: string}}
    parentID?: string
    previousID?: string
    retData?: any
    nextID?: string // Only used by insert
    isDetached?: boolean // Only used by insertAttrViewBlock
    srcIDs?: string[] // Only used by removeAttrViewBlock
    srcs?: IOperationSrcs[] // Only used by insertAttrViewBlock
    ignoreDefaultFill?: boolean // Only used by insertAttrViewBlock
    viewID?: string // Used by operations on multiple attribute views, so pushing does not affect the others
    name?: string // Only used by addAttrViewCol
    type?: TAVCol // Only used by addAttrViewCol
    deckID?: string // Only used by add/removeFlashcards
    blockIDs?: string[] // Only used by add/removeFlashcards
    removeDest?: boolean // Only used by removeAttrViewCol
    layout?: string // Only used by addAttrViewView
    groupID?: string // Only used by insertAttrViewBlock and sortAttrViewRow
    targetGroupID?: string // Only used by sortAttrViewRow
}

interface IOperationSrcs {
    itemID: string,
    id: string,
    content?: string,
    isDetached: boolean
}

interface IObject {
    [key: string]: string | number | boolean;
}

interface IHtmlToImageOptions {
    [key: string]: unknown;
    imagePlaceholder?: string;
    onImageErrorHandler?: (event: Event) => void;
}

interface ILayoutJSON extends ILayoutOptions {
    scrollAttr?: IScrollAttr,
    instance?: string,
    width?: string,
    height?: string,
    title?: string,
    lang?: string
    docIcon?: string
    page?: string
    path?: string
    blockId?: string
    mode?: TEditorMode
    action?: TProtyleAction
    icon?: string
    rootId?: string
    databaseRowId?: string
    active?: boolean
    pin?: boolean
    isPreview?: boolean
    customModelData?: any
    customModelType?: string
    config?: Config.IUILayoutTabSearchConfig
    children?: ILayoutJSON[] | ILayoutJSON
}

interface ICommand {
    langKey: string, // Key that identifies the command, also used as the i18n field name
    langText?: string, // Text to display, when set the i18n text of langKey is no longer used
    hotkey?: string, // Keyboard shortcut, empty string by default
    customHotkey?: string,
    callback?: () => void   // Not triggered when any of the other callbacks is set
    globalCallback?: () => void // Called when the focus is outside the app
    fileTreeCallback?: (file: import("../layout/dock/Files").Files) => void // Called when the focus is in the doc tree
    editorCallback?: (protyle: IProtyle) => void     // Called when the focus is in the editor
    dockCallback?: (element: HTMLElement) => void    // Called when the focus is in a dock
}

interface IPluginData {
    displayName: string,
    name: string,
    js: string,
    css: string,
    i18n: Record<string, string>
}

interface IPluginDockTab {
    position: TPluginDockPosition,
    size: Config.IUILayoutDockPanelSize,
    icon: string,
    hotkey?: string,
    title: string,
    index?: number
    show?: boolean
}

interface IExportOptions {
    type: string,
    id: string,
}

interface IOpenFileOptions {
    app: import("../index").App,
    searchData?: Config.IUILayoutTabSearchConfig, // Required for search
    // Required for card and custom tabs
    custom?: {
        title: string,
        icon: string,
        data?: any
        id: string,
        fn?: (options: {
            tab: import("../layout/Tab").Tab,
            data: any,
        }) => import("../layout/Model").Model,   // Kept for backwards compatibility with plugin 0.8.3
    }
    scrollPosition?: ScrollLogicalPosition,
    assetPath?: string, // Required for asset
    fileName?: string, // Required for file
    rootTitleEmpty?: boolean,
    rootIcon?: string, // Document icon
    id?: string,  // Required for file
    rootID?: string, // Required for file
    position?: string, // file or asset, where to open it
    page?: number | string, // asset
    mode?: TEditorMode // file
    action?: TProtyleAction[]
    keepCursor?: boolean // file, whether to move the focus to the new tab
    zoomIn?: boolean // Whether to zoom in on the block
    removeCurrentTab?: boolean // Whether the existing tab has to be removed when opening in the current tab
    openNewTab?: boolean // Open in a new tab
    afterOpen?: (model?: import("../layout/Model").Model) => void // Called after the file has been opened
}

interface ILayoutOptions {
    direction?: Config.TUILayoutDirection;
    size?: string;
    resize?: Config.TUILayoutDirection;
    type?: Config.TUILayoutType;
    element?: HTMLElement;
}

interface ITab {
    icon?: string;
    docIcon?: string;
    title?: string;
    panel?: string;
    callback?: (tab: import("../layout/Tab").Tab) => void;
}

interface IWebSocketData {
    cmd?: string;
    callback?: string;
    data?: any;
    msg: string;
    code: number;
    sid?: string;
    context?: any;
}

interface IGraphCommon {
    d3: {
        centerStrength: number
        collideRadius: number
        collideStrength: number
        lineOpacity: number
        linkDistance: number
        linkWidth: number
        nodeSize: number
        arrow: boolean
    };
    type: {
        blockquote: boolean
        callout: boolean
        code: boolean
        heading: boolean
        list: boolean
        listItem: boolean
        math: boolean
        paragraph: boolean
        super: boolean
        table: boolean
        tag: boolean
    };
}

interface IKeymapItem {
    default: string,
    custom: string
}

interface IFile {
    icon: string;
    name1: string;
    alias: string;
    memo: string;
    bookmark: string;
    path: string;
    name: string;
    titleEmpty?: boolean;
    hMtime: string;
    hCtime: string;
    hSize: string;
    dueFlashcardCount?: string;
    newFlashcardCount?: string;
    flashcardCount?: string;
    id: string;
    count: number;
    subFileCount: number;
}

interface IBlockTree {
    box: string,
    nodeType: string,
    hPath: string,
    subType: string,
    name: string,
    type: string,
    depth: number,
    url?: string,
    label?: string,
    id?: string,
    blocks?: IBlock[],
    count: number,
    children?: IBlockTree[]
}

interface IBlock {
    riffCard?: IRiffCard,
    depth?: number,
    box?: string;
    path?: string;
    hPath?: string;
    id?: string;
    rootID?: string;
    type?: string;
    content?: string;
    def?: IBlock;
    defID?: string
    defPath?: string
    refText?: string;
    name?: string;
    memo?: string;
    alias?: string;
    tag?: string;
    refs?: IBlock[];
    children?: IBlock[]
    length?: number
    ial: Record<string, string>
    refCount?: number
}

interface IRiffCard {
    due?: string;
    reps?: number; // Number of times the flashcard was reviewed
}

interface IModels {
    editor: import("../editor").Editor[],
    graph: import("../layout/dock/Graph").Graph[],
    outline: import("../layout/dock/Outline").Outline[]
    backlink: import("../layout/dock/Backlink").Backlink[]
    inbox: import("../layout/dock/Inbox").Inbox[]
    files: import("../layout/dock/Files").Files[]
    bookmark: import("../layout/dock/Bookmark").Bookmark[]
    tag: import("../layout/dock/Tag").Tag[]
    asset: import("../asset").Asset[]
    search: import("../search").Search[]
    custom: import("../layout/dock/Custom").Custom[]
}

interface IMenu {
    checked?: boolean,
    iconClass?: string,
    label?: string,
    click?: (element: HTMLElement, event: MouseEvent) => boolean | void | Promise<boolean | void>
    type?: "separator" | "submenu" | "readonly" | "empty",
    accelerator?: string,
    action?: string,
    id?: string,
    submenu?: IMenu[]
    disabled?: boolean
    icon?: string
    iconHTML?: string
    current?: boolean
    bind?: (element: HTMLElement) => void
    index?: number
    element?: HTMLElement
    ignore?: boolean
    warning?: boolean
}

interface IBazaarItem {
    preferredName: string;
    minAppVersion: string;
    preferredDesc: string;
    preferredReadme: string;
    iconURL: string;
    stars: string;
    author: string;
    updated: string;
    downloads: string;
    disallowInstall: boolean;
    current: false;
    installed: false;
    outdated: false;
    name: string;
    previewURL: string;
    repoHash: string;
    repoURL: string;
    url: string;
    openIssues: number;
    version: string;
    hSize: string;
    hInstallSize: string;
    hInstallDate: string;
    hUpdated: string;
    preferredFunding: string;
    disallowUpdate: boolean;
    updateRequiredMinAppVer?: string;
    installedIncompatible?: boolean; // plugin only
    bazaarIncompatible?: boolean; // plugin only
    enabled?: boolean; // plugin only
    modes?: string[]; // theme only
}

interface IAV {
    id: string;
    name: string;
    view: IAVTable | IAVGallery;
    viewID: string;
    viewType: TAVView;
    views: IAVView[];
    isMirror?: boolean;
    newItemTemplates?: IAVNewItemTemplate[];
    defaultTemplateID?: string;
    target?: IAVRenderTarget;
}

interface IAVRenderTarget {
    status: "visible" | "filtered" | "itemNotFound" | "viewNotFound" | "groupHidden";
    itemID: string;
    groupID?: string;
    index: number;
    offset: number;
    pageSize: number;
}

type TAVNewItemTarget = "detached" | "document";
type TAVNewItemFieldValueMode = "static" | "currentTime";

interface IAVNewItemSaveLocation {
    boxID?: string;
    pathTemplate: string;
}

interface IAVNewItemFieldValue {
    mode: TAVNewItemFieldValueMode;
    value?: IAVCellValue;
}

interface IAVNewItemTemplate {
    id: string;
    name: string;
    icon?: string;
    targetType: TAVNewItemTarget;
    primaryKeyTemplate?: string;
    fieldValues?: Record<string, IAVNewItemFieldValue>;
    saveLocation?: IAVNewItemSaveLocation;
    contentTemplatePath?: string;
}

interface IAVView {
    name: string;
    desc: string;
    id: string;
    type: TAVView;
    icon: string;
    hideAttrViewName: boolean;
    pageSize: number;
    showIcon: boolean;
    wrapField: boolean;
    groupHidden?: number,  // 0: shown, 1: hidden because empty, 2: hidden manually
    groupFolded?: boolean,
    filters: IAVFilter[],
    sorts: IAVSort[],
    groups: IAVView[]
    group: IAVGroup
    groupKey: IAVColumn
    groupValue: IAVCellValue
}

interface IAVTable extends IAVView {
    columns: IAVColumn[],
    rows: IAVRow[],
    rowCount: number,
}

interface IAVVirtualData {
    renderedStart: number;
    renderedEnd: number;
    topSpacerHeight: number;
    rowOffset?: number;
    locate?: boolean;
}

interface IAVGallery extends IAVView {
    coverFrom: number;    // 0: none, 1: image in the content, 2: asset field, 3: content block
    coverFromAssetKeyID?: string;
    cardSize: number;   // 0: small card, 1: medium card, 2: large card
    cardAspectRatio: number;
    displayFieldName: boolean;
    fitImage: boolean;
    cards: IAVGalleryItem[],
    desc: string
    fields: IAVColumn[]
    cardCount: number,
}

interface IAVKanban extends IAVView {
    coverFrom: number;    // 0: none, 1: image in the content, 2: asset field, 3: content block
    coverFromAssetKeyID?: string;
    cardSize: number;   // 0: small card, 1: medium card, 2: large card
    cardAspectRatio: number;
    displayFieldName: boolean;
    fitImage: boolean;
    cards: IAVGalleryItem[],
    desc: string
    fields: IAVColumn[]
    cardCount: number,
    fillColBackgroundColor: boolean
}

interface IAVFilter {
    column?: string,                                  // Leaf node: field (column) ID
    operator?: TAVFilterOperator,                     // Leaf node: operator
    quantifier?: string,                              // Leaf node: quantifier
    value?: IAVCellValue,                             // Leaf node: value to filter on
    relativeDate?: IAVRelativeDate,                   // Leaf node: relative date
    relativeDate2?: IAVRelativeDate,                  // Leaf node: second relative date
    combination?: "and" | "or",                       // Group node: how the child conditions are combined
    filters?: IAVFilter[],                            // Group node: child nodes (recursive)
}

interface IAVRelativeDate {
    count: number;   // Amount
    unit: number;    // Unit: 0: day, 1: week, 2: month, 3: year
    direction: number;   // Direction: -1: past, 0: now, 1: future
}

interface IAVGroup {
    field: string,
    method?: number //  0: value, 1: number range, 2: relative date, 3: day, 4: week, 5: month, 6: year
    range?: {
        numStart: number // Start of the number range, e.g. 0
        numEnd: number   // End of the number range, e.g. 1000
        numStep: number  // Step of the number range, e.g. 100
    }
    hideEmpty?: boolean
    order?: number  // Ascending: 0 (default), descending: 1, manual: 2, by option: 3
}

interface IAVSort {
    column: string,
    order: "ASC" | "DESC"
}

interface IAVColumn {
    width: string,
    align: TAVAlign,
    icon: string,
    id: string,
    name: string,
    desc: string,
    wrap: boolean,
    pin: boolean,
    hidden: boolean,
    type: TAVCol,
    numberFormat: string,
    template: string,
    calc: IAVCalc,
    updated?: {
        includeTime: boolean
    }
    created?: {
        includeTime: boolean
    }
    date?: {
        autoFillNow: boolean,
        fillSpecificTime: boolean,
    }
    // List of options
    options?: {
        name: string,
        color: string,
        desc?: string,
    }[],
    relation?: IAVColumnRelation,
    rollup?: IAVCellRollupValue
}

interface IAVRow {
    id: string,
    cells: IAVCell[]
}

interface IAVGalleryItem {
    coverURL?: string;
    coverContent?: string;
    id: string;
    values: IAVCell[];
}

interface IAVCell {
    id: string,
    color: string,
    bgColor: string,
    value: IAVCellValue,
    valueType: TAVCol,
}

interface IAVCellValue {
    keyID?: string,
    id?: string,
    blockID?: string // The row ID
    type: TAVCol,
    isDetached?: boolean,
    text?: {
        content: string
    },
    number?: {
        content?: number,
        isNotEmpty: boolean,
        format?: string,
        formattedContent?: string
    },
    mSelect?: IAVCellSelectValue[]
    mAsset?: IAVCellAssetValue[]
    block?: {
        content: string,
        id?: string,
        icon?: string
    }
    url?: {
        content: string
    }
    phone?: {
        content: string
    }
    email?: {
        content: string
    }
    template?: {
        content: string
    },
    checkbox?: {
        checked: boolean,
        content?: string, // Shown in the gallery https://github.com/siyuan-note/siyuan/issues/15389
    }
    relation?: IAVCellRelationValue
    rollup?: {
        contents?: IAVCellValue[]
    }
    date?: IAVCellDateValue
    created?: IAVCellDateValue
    updated?: IAVCellDateValue
}

interface IAVCellRelationValue {
    blockIDs: string[];
    contents?: IAVCellValue[];
}

interface IAVCellDateValue {
    content?: number,
    isNotEmpty?: boolean
    content2?: number,
    isNotEmpty2?: boolean
    hasEndDate?: boolean
    formattedContent?: string,
    isNotTime?: boolean // Defaults to true
}

interface IAVCellSelectValue {
    content: string,
    color: string
}

interface IAVCellAssetValue {
    content: string,
    name: string,
    type: "file" | "image"
}

interface IAVColumnRelation {
    avID?: string;
    backKeyID?: string;
    isTwoWay?: boolean;
}

interface IAVCellRollupValue {
    relationKeyID?: string;  // ID of the relation column
    keyID?: string;
    calc?: IAVCalc;
}

interface IAVCalc {
    operator?: string,
    template?: string,
    result?: IAVCellValue
}

interface IPublishAccessItem {
    id: string,
    visible: boolean,
    password: string,
    disable: boolean
    iconHTML?: string
}

interface IKernelPlugin {
    /**
     * State of the kernel plugin
     */
    state: IKernelPluginState;

    /**
     * JSON-RPC interface used to call into the kernel plugin
     */
    rpc: IKernelPluginRpc;
}

interface IKernelPluginState {
    /**
     * Current state of the kernel plugin
     */
    code: TKernelPluginState;

    /**
     * Human readable description of the kernel plugin state
     */
    description: string;
}

interface IKernelPluginRpcCall {
    /**
     * In JSON-RPC 2.0 the method must be a string. It is up to the plugin developer to make sure the name matches a
     * method the kernel plugin has bound, otherwise the call may fail.
     */
    method: TJsonRpcMethod;

    /**
     * In JSON-RPC 2.0 the id may be a string, a number or null, but for compatibility and practical reasons the plugin
     * system does not allow null to be used as an id.
     *
     * When it is omitted and notification is not true, a unique id is generated automatically. When it is provided it
     * must be unique, otherwise responses may be wrong or get mixed up.
     */
    id?: TJsonRpcId;

    /**
     * In JSON-RPC 2.0 the params may be an array or an object. It is up to the plugin developer to make sure the
     * arguments match the parameters of the method the kernel plugin has bound.
     */
    params?: any[] | Record<string, any>;

    /**
     * Whether this is a notification. A notification gets no response and must not carry an id.
     * @defaultValue false
     */
    notification?: boolean;
}

interface IKernelPluginRpcRequest extends IKernelPluginRpcCall {
    jsonrpc: "2.0";
}

interface IKernelPluginRpcBaseResponse {
    jsonrpc: "2.0";
}

interface IKernelPluginRpcResultResponse extends IKernelPluginRpcBaseResponse {
    id: TJsonRpcId;
    result?: any;
}

interface IKernelPluginRpcErrorResponse extends IKernelPluginRpcBaseResponse {
    id: TJsonRpcId | null;
    error?: any;
}

interface IKernelPluginRpcError {
    code: number;
    message: string;
    data?: any;
}

interface IKernelPluginRpc {
    /**
     * Dynamic method dispatch backed by a {@link Proxy}. Call `call.methodName(params)` to invoke a method exposed by
     * the kernel plugin without having to deal with the JSON-RPC details.
     */
    call: Record<TJsonRpcMethod, (...args: TJsonRpcMethodParams) => Promise<any>>;

    /**
     * Dynamic method dispatch backed by a {@link Proxy}. Call `notify.methodName(...args)` to send a notification to
     * the kernel plugin without having to deal with the JSON-RPC details.
     */
    notify: Record<TJsonRpcMethod, (...args: TJsonRpcMethodParams) => void>;

    /**
     * Batch call. Takes a list of method calls and resolves to a list of results, one entry per non-notification call
     * in the same order, each holding either the result or the error.
     */
    batch: (...calls: IKernelPluginRpcCall[]) => Promise<IKernelPluginRpcError | (IKernelPluginRpcResultResponse | IKernelPluginRpcErrorResponse)[]>;

    /**
     * Registers an event handler. Use `bind("methodName", handler)` to listen for the notifications the kernel plugin
     * pushes to the client plugin over JSON-RPC.
     */
    bind: (method: TJsonRpcMethod, handler: TJsonRpcHandler<void>) => void;

    /**
     * Removes an event handler. Use `unbind("methodName", handler)` to stop listening for the notifications the kernel
     * plugin pushes to the client plugin over JSON-RPC.
     */
    unbind: (method: TJsonRpcMethod, handler: TJsonRpcHandler<void>) => void;
}

/**
 * Block information carried by a SiYuan URI, describing the block referenced through the SiYuan URI protocol
 */
interface ISiYuanUriBlockInfo {
    /**
     * Block ID
     */
    id: string;

    /**
     * Whether to focus the block
     *
     * @defaultValue false
     */
    focus: boolean;

    /**
     * Whether to display the block in fullscreen
     *
     * @defaultValue false
     */
    fullscreen: boolean;
    avItemID?: string;
    avViewID?: string;
    avGroupID?: string;
}
