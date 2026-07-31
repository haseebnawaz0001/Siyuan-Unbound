import {Tab} from "../../Tab";
import {Model} from "../../Model";
import {App} from "../../../index";
import {AgentHttpError, fetchAgentSSE, IEditorContext, ISSEResult, IToolEffects} from "./agentSSE";
import {genUUID} from "../../../util/genID";
import {mountComposer} from "./AgentComposer";
import {disabledWYSIWYG} from "../../../protyle/util/onGet";
import {getAllEditor} from "../../getAll";
import "./frontendActions";
import {listActions, lookupAction} from "./frontendActions";
import {AgentSession, SessionStore} from "./SessionStore";
import {AgentSessionPanel} from "./AgentSessionPanel";
import {getDockByType} from "../../tabUtil";
import {updateHotkeyAfterTip} from "../../../protyle/util/compatibility";
import {getAgentLute} from "../../../protyle/render/setLute";
import {setPanelFocus} from "../../util";
import {escapeAriaLabel, escapeHtml} from "../../../util/escape";
import {setPosition} from "../../../util/setPosition";
import {fetchPost} from "../../../util/fetch";
import {confirmDialog} from "../../../dialog/confirmDialog";
import {showMessage} from "../../../dialog/message";
import * as dayjs from "dayjs";
import {sendNotification} from "../../../plugin/platformUtils";
import {
    findAgentUserEntryIndex,
    filterAgentReferencesForContent,
    hasAgentExecutedToolsAfter,
    isAgentRegenerateStateCurrent
} from "./AgentHistory";
import {
    bindThinkingCardToggle,
    createThinkingCardElement,
    postRender,
    renderQuestionCardHTML,
    renderRetryCardHTML,
    renderTodoList,
    renderToolsLineHTML,
    renderWelcomeHTML
} from "./AgentMessageRenderer";

// Limit the number of visible block IDs injected into the user turn context to control token cost.
// Kept in sync with maxVisibleBlockIDs in kernel/agent/agent.go.
const maxVisibleBlockIDs = 50;

type EntryBase = { id?: string };
type AgentReference = { id: string; title: string };
type UserEntry = EntryBase & {
    type: "user";
    content: string;
    blockHTML?: string;
    references?: AgentReference[];
    editorContext?: IEditorContext;
    timestamp?: number
};

type SessionEntry =
    | UserEntry
    | (EntryBase & {
    type: "thinking";
    steps: Array<{
        reasoning: string;
        reasoningContent: string;
        toolNames?: string[];
        content?: string
    }>;
    duration?: number
})
    | (EntryBase & {
    type: "assistant";
    content?: string;
    toolCalls?: Array<{ name: string; arguments: Record<string, unknown>; result?: string; state?: string }>;
    timestamp?: number
})
    | (EntryBase & { type: "confirm"; name: string; args: Record<string, unknown>; confirmID: string; effects?: IToolEffects; status?: string })
    | (EntryBase & { type: "question"; questionID: string; questions: Array<Record<string, unknown>>; status?: string; answers?: string[] })
    | (EntryBase & { type: "snapshot"; snapshotID: string })
    | (EntryBase & { type: "rollback"; snapshotID: string });

export class AgentChat extends Model {
    private messagesContainer: HTMLElement;
    private composerHost: HTMLElement;
    private composer: ReturnType<typeof mountComposer> | null = null;
    private sendBtn: HTMLElement;
    private stopBtn: HTMLElement;
    private newSessionBtn: HTMLElement;
    private titleElement: HTMLElement;
    private sessionMenuBtn: HTMLElement;
    private sessionPanel: AgentSessionPanel;
    private sessionId = "";
    private sessionTitle = "";
    private pendingSessionTitle: string | null = null;
    private entries: SessionEntry[] = [];
    private hasTitled = false;
    private isStreaming = false;
    private currentAIElement: HTMLElement | null = null;
    private currentAssistantEntryId = "";
    private currentThinkingEntryId = "";
    private currentTurnID = "";
    private recoveryCommitTurnIDs = new Map<string, string>();
    private pendingRecoverySessionIDs = new Set<string>();
    private recoveryInFlightSessionIDs = new Set<string>();
    private lute: Lute;
    private currentContent = "";
    private fullContent = "";
    private contextTokens = 0;
    private contextTokenBreakdown: Record<string, number> = {};
    private contextCachedTokens = 0;
    private contextLimit = 0;
    private tokenPopup: HTMLElement | null = null;
    private tokenPopupShowTimer = 0;
    private tokenPopupHideTimer = 0;
    private tokenPopupOutsideClickHandler: (() => void) | null = null;
    private tokenPopupResizeHandler: (() => void) | null = null;
    private sessionCreatedAt = 0;
    private requestStartTime = 0;
    private tokenDisplayEl: HTMLElement;
    private defaultTitle = "";
    private currentToolCalls: Array<{ name: string; arguments: Record<string, unknown>; result?: string }> = [];
    private toolCallStartedAt = new Map<string, number>();
    private abortController: AbortController | null = null;
    private currentThinkingText = "";
    private currentThinkingReasoning = "";
    private currentThinkingReasoningContent = "";
    private editingUserEntryID = "";
    private pendingEditDraft: { entryID: string; content: string } | null = null;
    // A thinking step only keeps the tool name list (deduped: arguments/result are stored once,
    // in the assistant entry); it no longer stores text ("Thought for Xs" is generated from
    // duration via i18n at render time).
    private currentThinkingSteps: Array<{
        reasoning: string;
        reasoningContent: string;
        toolNames?: string[];
        content?: string
    }> = [];
    // Thinking duration of the current request, in seconds. Persisted as entry.duration; the
    // "Thought for" text itself is not persisted.
    private currentThinkingDuration = 0;
    private currentThinkingStepContent = "";
    private pendingConfirms: SessionEntry[] = [];
    private renderedToolNames: Record<string, boolean> = {};
    private hasInterveningCard = false;
    private modelSelect: HTMLSelectElement;
    private selectedModel: string;
    private modelOptions: Array<{ id: string; name: string }> = [];
    // Reasoning effort (iconBrain + native select), remembered only for this instance; resets to
    // default after a refresh.
    private reasoningEffortSelect: HTMLSelectElement;
    private selectedReasoningEffort = "";
    private userScrolledUp = false;
    private programmaticScroll = false;
    private stickResizeObserver: ResizeObserver | null = null;
    // Distance from the bottom (scrollHeight - scrollTop), saved per session, used to restore the
    // scroll position after switching sessions or toggling the dock panel.
    // We use the distance from the bottom rather than the absolute scrollTop: the dock has a
    // width/height transition while expanding/collapsing, during which scrollHeight changes, but
    // the distance from the bottom is unaffected, so restoring it lands at the same relative spot.
    private scrollBottomBySession: Map<string, number> = new Map();
    // Panel visibility: when the dock is closed the container size collapses to zero and the
    // browser clamps scrollTop to 0, so the scroll position is not recorded while collapsed.
    private layoutVisible = true;
    private layoutResizeObserver: ResizeObserver | null = null;
    private settingDialogObserver: MutationObserver | null = null;
    private scrollBottomBtn: HTMLElement;
    private navRail: HTMLElement;
    private navExpandTimer = 0;
    // Mirror state: the current session is being streamed by another instance, and this instance
    // is locked into a read-only placeholder and does not redraw the current view meanwhile.
    // Driven by the ws streamStart/streamEnd events, mutually exclusive with the initiator's
    // isStreaming (the initiator uses SSE).
    private mirrorLocked = false;
    private mirrorPlaceholderEl: HTMLElement | null = null;
    // Thinking timer: while streaming, refreshes the title of unfinished thinking cards to
    // "Thinking... X.Xs" every 100ms.
    private thinkingTimerId = 0;
    // Baseline length of currentToolCalls at the time of the previous thinking step snapshot,
    // used to compute the tools newly added this round (avoids step.toolNames accumulating
    // duplicate historical tools).
    private lastStepToolCount = 0;

    constructor(app: App, tab: Tab) {
        super({app: app});
        this.parent = tab;
        this.lute = getAgentLute({
            emojiSite: "/emojis",
            emojis: {}
        });
        this.defaultTitle = window.siyuan.languages.agentChat || "Agent";
        this.sessionTitle = this.defaultTitle;
        this.initUI();
        this.bindEvents();
        // Connect to ws to receive cross-instance session change notifications (agentSessionChanged).
        // The AgentChat dock is a persistent singleton, so its ws connection persists along with it,
        // consistent with existing docks like Backlink/Bookmark.
        this.connect({
            id: genUUID(),
            type: "agentChat",
            msgCallback: (data) => this.onWsMessage(data),
        });
        // Saving the AI config goes through a local patch (aiRuntime.ts writes
        // window.siyuan.config.ai) which does not broadcast over ws, so we fall back to two
        // mechanisms: window focus (cross-window) + a MutationObserver watching for the settings
        // dialog closing (immediate, same-window).
        window.addEventListener("focus", this.checkConfigChangedHandler);
        // The settings dialog is an internal SiYuan modal; closing it does not blur the window, so
        // the focus event never fires.
        // Watch for changes to body's children, and refresh immediately when the settings dialog
        // containing .config__panel is removed.
        this.settingDialogObserver = new MutationObserver(() => {
            if (!document.querySelector(".config__panel")) {
                this.checkConfigChanged();
            }
        });
        this.settingDialogObserver.observe(document.body, {childList: true, subtree: false});
    }

    private checkConfigChangedHandler = () => {
        this.checkConfigChanged();
    };

    // Compare the actual number of usable models in window.siyuan.config.ai against the cached
    // modelOptions, and refresh if they differ.
    // Only re-render when on the welcome page (no session content), so it can switch between the
    // no-model prompt block and the examples; when there is session content, do not redraw (to
    // avoid disrupting the conversation) — refreshModelOptions already refreshes the trigger display.
    private checkConfigChanged() {
        const actualCount = AgentChat.countUsableModels(window.siyuan.config.ai);
        if (actualCount === this.modelOptions.length) {
            return;
        }
        this.refreshModelOptions();
        if (this.entries.length === 0 && this.messagesContainer.querySelector(".agent-welcome")) {
            this.showWelcome();
        }
    }

    // Count of "usable models", consistent with refreshModelOptions / the backend HasAnyProvider().
    private static countUsableModels(aiConfig: Config.IAI): number {
        let count = 0;
        for (const prov of aiConfig.providers || []) {
            if (!prov.enabled) {
                continue;
            }
            for (const m of prov.models) {
                if (m.enabled && (m.displayName || m.name)) {
                    count++;
                }
            }
        }
        return count;
    }

    private initUI() {
        const panel = this.parent.panelElement;
        panel.classList.add("fn__flex-column", "file-tree", "sy__agentChat", "dockPanel");

        const L = window.siyuan.languages;

        panel.innerHTML = '<div class="agent-chat fn__flex-column fn__flex-1">' +
            '<div class="block__icons fn__hidescrollbar">' +
            '<div class="block__logo fn__flex-1 agent-chat__title">' + (L.agentChat || "Agent") + "</div>" +
            '<span data-type="new-session" class="block__icon ariaLabel" data-position="north" aria-label="' + (L.agentNewSession || "New Session") + '">' +
            '<svg><use xlink:href="#iconAdd"></use></svg>' +
            "</span>" +
            '<span class="fn__space"></span>' +
            '<span data-type="session-menu" class="block__icon ariaLabel" data-position="north" aria-label="' + L.manageSessions + '">' +
            '<svg><use xlink:href="#iconFolderClock"></use></svg>' +
            "</span>" +
            '<span class="fn__space"></span>' +
            '<span data-type="min" class="block__icon ariaLabel" data-position="north" aria-label="' + window.siyuan.languages.min + updateHotkeyAfterTip(window.siyuan.config.keymap.general.closeTab.custom) + '">' +
            '<svg><use xlink:href="#iconMin"></use></svg>' +
            "</span>" +
            "</div>" +
            '<div class="agent-chat__messages-wrap">' +
            '<div class="agent-chat__messages fn__flex-1"></div>' +
            '<span class="agent-chat__scroll-bottom ariaLabel" data-position="west" aria-label="' + L.scrollToBottom + '"><svg><use xlink:href="#iconArrowDown"></use></svg></span>' +
            "</div>" +
            '<div class="agent-chat__input-area">' +
            '<div class="agent-chat__composer-host"></div>' +
            '<div class="agent-chat__buttons">' +
            '<span class="fn__flex-1"></span>' +
            '<span class="agent-chat__tokens fn__none b3-button b3-button--icon b3-button--cancel" aria-label="' + (L.tokenUsage || "Context Usage") + '">' +
            '<svg viewBox="0 0 24 24">' +
            '<circle class="agent-chat__tokens-track" cx="12" cy="12" r="9" stroke-width="3"></circle>' +
            '<circle class="agent-chat__tokens-arc" cx="12" cy="12" r="9" stroke-width="3" stroke-dasharray="0 56.55"></circle>' +
            "</svg>" +
            "</span>" +
            '<select class="b3-select b3-select--noborder" tabindex="0"></select>' +
            '<div class="b3-form__icon ariaLabel" aria-label="' + (L.reasoningEffortTooltip || "Reasoning effort") + '"><svg class="b3-form__icon-icon"><use xlink:href="#iconBrain"></use></svg><select class="b3-select b3-select--noborder b3-form__icon-input" tabindex="0"></select></div>' +
            '<button class="agent-chat__send b3-button b3-button--icon b3-button--text ariaLabel" aria-label="' + (L.agentSend || "Send") + '"><svg><use xlink:href="#iconSend"></use></svg></button>' +
            '<button class="agent-chat__stop b3-button b3-button--icon b3-button--cancel fn__none ariaLabel" aria-label="' + (L.agentStop || "Stop") + '"><svg><use xlink:href="#iconSquareStop"></use></svg></button>' +
            "</div>" +
            "</div>" +
            '<div class="agent-chat__preview-notice">' + (L.featurePreview || "") + "</div>" +
            "</div>";

        this.messagesContainer = panel.querySelector(".agent-chat__messages") as HTMLElement;
        this.composerHost = panel.querySelector(".agent-chat__composer-host") as HTMLElement;
        this.sendBtn = panel.querySelector(".agent-chat__send") as HTMLElement;
        this.stopBtn = panel.querySelector(".agent-chat__stop") as HTMLElement;
        this.newSessionBtn = panel.querySelector('.block__icon[data-type="new-session"]') as HTMLElement;
        this.sessionMenuBtn = panel.querySelector('.block__icon[data-type="session-menu"]') as HTMLElement;
        this.titleElement = panel.querySelector(".agent-chat__title") as HTMLElement;
        this.tokenDisplayEl = panel.querySelector(".agent-chat__tokens") as HTMLElement;
        this.modelSelect = panel.querySelector(".b3-select") as HTMLSelectElement;
        this.reasoningEffortSelect = panel.querySelector(".b3-form__icon-input") as HTMLSelectElement;
        this.initReasoningEffortSelect();
        this.scrollBottomBtn = panel.querySelector(".agent-chat__scroll-bottom") as HTMLElement;
        this.messagesContainer.addEventListener("scroll", () => {
            const {scrollTop, scrollHeight, clientHeight} = this.messagesContainer;
            // Only record the scroll position while the panel is actually expanded: during the
            // dock's collapse transition the container size drops to zero and the browser clamps
            // scrollTop to 0, which fires a scroll event; without excluding this by size, it would
            // pollute the saved distance from the bottom.
            // Use clientHeight to decide (0 while collapsed), which is more reliable than the
            // layoutVisible flag — the latter is set asynchronously by the ResizeObserver and can
            // race with this event.
            if (this.layoutVisible && clientHeight > 0 && this.sessionId) {
                this.scrollBottomBySession.set(this.sessionId, scrollHeight - scrollTop);
            }
            if (this.programmaticScroll) {
                return;
            }
            const distanceFromBottom = scrollHeight - scrollTop - clientHeight;
            // Hysteresis: only mark as scrolled-up when clearly above bottom (>=60),
            // and only return to sticky when really at the bottom (<=10).
            // This prevents the follow state from rapidly toggling while streaming
            // causes scrollHeight to grow asynchronously.
            if (!this.userScrolledUp) {
                this.userScrolledUp = distanceFromBottom >= 60;
            } else if (distanceFromBottom <= 10) {
                this.userScrolledUp = false;
            }
            this.scrollBottomBtn.classList.toggle("agent-chat__scroll-bottom--visible", this.userScrolledUp);
            this.updateActiveMarker();
        }, {passive: true});

        const messagesWrap = panel.querySelector(".agent-chat__messages-wrap") as HTMLElement;
        this.initNavRail(messagesWrap);

        this.initModelSelect();

        this.composer = mountComposer(this.composerHost, () => {
            this.sendMessage();
        }, () => {
            // Refresh the send button's enabled state whenever content changes (covers all doc
            // changes: user input, IME, programmatic clear, etc.).
            this.updateSendButtonState();
        });
        // Block drag-and-drop is handled uniformly by protyle (copy block/ref/embed block ->
        // reference), so no custom drop handler is needed here.
        this.sessionPanel = new AgentSessionPanel(
            this.sessionMenuBtn,
            this.parent.panelElement,
            () => this.sessionId,
            () => this.defaultTitle,
            {
                onSwitch: (id) => this.switchSession(id),
                onDelete: (id) => this.deleteSession(id),
                onRename: async (id, title) => {
                    if (id === this.sessionId) {
                        this.sessionTitle = title;
                        this.titleElement.textContent = title;
                        // The content revision number must stay the same before and after this turn
                        // starts. While streaming, only update the local title; it gets persisted
                        // together with the final commit, to avoid a metadata save prematurely
                        // bumping the revision number.
                        if (this.isStreaming || this.currentTurnID) {
                            this.pendingSessionTitle = title;
                            return;
                        }
                    }
                    await SessionStore.rename(id, title);
                },
            }
        );
        // Watch the scroll container's size: when the dock panel collapses the container size
        // drops to zero and the browser clamps scrollTop to 0; here we restore the current
        // session's scroll position once the panel expands again. The dock's expand/collapse has a
        // CSS width/height transition (~0.2s), during which scrollHeight changes with the size, so
        // after a collapse -> expand transition we keep correcting via an rAF loop for about 320ms,
        // covering the transition animation until the layout settles.
        this.layoutResizeObserver = new ResizeObserver(() => {
            const collapsed = this.messagesContainer.clientWidth === 0 || this.messagesContainer.clientHeight === 0;
            if (collapsed) {
                this.layoutVisible = false;
                return;
            }
            // Only start a correction loop right when recovering from collapsed, to avoid
            // interfering with normal scrolling / streaming output.
            if (!this.layoutVisible) {
                this.layoutVisible = true;
                const saved = this.scrollBottomBySession.get(this.sessionId) ?? 0;
                this.restoreScrollToBottom(saved);
            }
        });
        this.layoutResizeObserver.observe(this.messagesContainer);

        this.initSessions();
    }

    private initModelSelect() {
        this.refreshModelOptions();
        // Selected model change: the native select's change event, no custom menu logic needed.
        this.modelSelect.addEventListener("change", () => {
            this.selectedModel = this.modelSelect.value;
        });
        // When there are no models, intercept the dropdown from opening and open the AI settings
        // panel instead (dynamic import avoids a circular dependency).
        this.modelSelect.addEventListener("mousedown", (e: MouseEvent) => {
            if (this.modelOptions.length > 0) {
                return;
            }
            e.preventDefault();
            this.openAiSetting();
        });
    }

    // Open the settings panel and switch to the "AI" tab. Dynamically import the config module to
    // avoid a circular dependency with AgentChat.
    private async openAiSetting() {
        const {openSetting} = await import("../../../config");
        // openSetting destroys and rebuilds an existing settings dialog, so check for an existing
        // one first and reuse it to avoid a flicker.
        const existing = window.siyuan.dialogs.find(d => d.element.querySelector(".config__tab-container"));
        if (!existing) {
            openSetting(this.app, "ai");
        }
    }

    // Append external block references to the end of the composer as mention chips, equivalent to
    // dragging a block into the composer or picking one via @ search inside it.
    public insertBlockMentions(mentions: Array<{ id: string; label: string }>) {
        if (this.composer && mentions.length > 0) {
            this.composer.insertMentions(mentions);
        }
    }

    // Recompute the list of usable models from window.siyuan.config.ai; idempotent, safe to call
    // repeatedly.
    // Consistent with the backend HasAnyProvider()/GetModel() logic: both provider and model must
    // be enabled.
    // With zero models, explicitly clear selectedModel (to avoid passing undefined through to the
    // backend); a stale selection is reset automatically.
    refreshModelOptions() {
        const aiConfig = window.siyuan.config.ai;
        const newOptions: Array<{ id: string; name: string }> = [];
        for (const prov of aiConfig.providers || []) {
            if (!prov.enabled) {
                continue;
            }
            for (const m of prov.models) {
                if (!m.enabled) {
                    continue;
                }
                const displayName = m.displayName || m.name;
                if (!displayName) {
                    continue;
                }
                newOptions.push({id: m.id || m.name, name: displayName});
            }
        }
        this.modelOptions = newOptions;
        // If the current selection is no longer valid (not in the new list), reset it: pick the
        // first model if any exist, otherwise explicitly clear it.
        const stillValid = this.selectedModel && newOptions.some(o => o.id === this.selectedModel);
        if (!stillValid) {
            this.selectedModel = newOptions.length > 0 ? newOptions[0].id : "";
        }
        this.updateModelLabel();
        this.updateSendButtonState();
    }

    private updateModelLabel() {
        // Rebuild the <option> list. When there are no usable models, insert a placeholder item;
        // clicking the select opens the AI settings.
        let html = "";
        if (this.modelOptions.length === 0) {
            const placeholder = window.siyuan.languages.noModelConfigured || "No model configured";
            html = '<option value="" selected>' + escapeHtml(placeholder) + "</option>";
            this.modelSelect.innerHTML = html;
            this.modelSelect.disabled = true;
            return;
        }
        this.modelSelect.disabled = false;
        for (const o of this.modelOptions) {
            html += '<option value="' + escapeHtml(o.id) + '">' + escapeHtml(o.name) + "</option>";
        }
        this.modelSelect.innerHTML = html;
        this.modelSelect.value = this.selectedModel;
    }

    private getSelectedModel(): string {
        return this.selectedModel;
    }

    // Refresh the button's text (Default/Low/Medium/High) based on the current selection.
    // Initialize the native reasoning-effort select: populate the 4 options and bind change,
    // following the same pattern as initModelSelect.
    private initReasoningEffortSelect() {
        const L = window.siyuan.languages;
        const options: Array<{ value: string; label: string }> = [
            {value: "", label: L.reasoningEffortDefault || "Default"},
            {value: "low", label: L.reasoningEffortLow || "Low"},
            {value: "medium", label: L.reasoningEffortMedium || "Medium"},
            {value: "high", label: L.reasoningEffortHigh || "High"},
        ];
        this.reasoningEffortSelect.innerHTML = options
            .map(o => '<option value="' + escapeHtml(o.value) + '">' + escapeHtml(o.label) + "</option>")
            .join("");
        this.reasoningEffortSelect.value = this.selectedReasoningEffort;
        this.reasoningEffortSelect.addEventListener("change", () => {
            this.selectedReasoningEffort = this.reasoningEffortSelect.value;
        });
    }

    // Validate whether the model ID persisted with the session still exists in the current config.
    // Assign and refresh the label if valid, otherwise keep the current selection.
    // This avoids passing a stale ID of a deleted model through to the backend when loading an old
    // session, which would otherwise fail silently.
    private applySessionModelIfValid(modelId?: string) {
        if (modelId && this.modelOptions.some(o => o.id === modelId)) {
            this.selectedModel = modelId;
        }
        this.updateModelLabel();
    }

    private showWelcome() {
        this.editingUserEntryID = "";
        const hasModel = this.modelOptions.length > 0;
        this.messagesContainer.innerHTML = renderWelcomeHTML(hasModel);
        if (!hasModel) {
            // No models: bind the "Go configure" button, clicking it opens the AI settings panel.
            const goBtn = this.messagesContainer.querySelector(".agent-welcome__go-setting");
            if (goBtn) {
                goBtn.addEventListener("click", () => {
                    this.openAiSetting();
                });
            }
            return;
        }
        const examples = this.messagesContainer.querySelectorAll(".agent-welcome__example");
        examples.forEach((example) => {
            const ex = example as HTMLElement;
            ex.addEventListener("click", async () => {
                const text = ex.getAttribute("data-text") || "";
                if (text && this.composer) {
                    this.messagesContainer.innerHTML = "";
                    const userEntryId = SessionStore.newSessionId();
                    this.entries.push({id: userEntryId, type: "user", content: text, timestamp: Date.now()});
                    this.appendUserMessage(text, Date.now(), userEntryId);
                    this.rebuildNavMarkers();
                    this.tryGenerateTitle();
                    this.setStreaming(true);
                    try {
                        await this.saveSession();
                    } catch (e) {
                        this.rollbackUserEntry(userEntryId);
                        this.setStreaming(false);
                        await this.reloadFromDisk();
                        return;
                    }
                    this.abortController = new AbortController();
                    const requestSessionId = this.sessionId;
                    this.requestStartTime = Date.now();
                    this.currentThinkingDuration = 0;
                    this.currentTurnID = "";
                    await fetchAgentSSE(text, window.siyuan.config.appearance.lang, [],
                        (event: ISSEResult) => {
                            if (this.sessionId !== requestSessionId) {
                                return;
                            }
                            return this.handleSSEEvent(event);
                        },
                        (err: Error) => {
                            if (this.sessionId !== requestSessionId) {
                                return;
                            }
                            if (err instanceof AgentHttpError && err.status === 409) {
                                return this.handleConflictReject();
                            }
                            return this.handleConfigError(err, userEntryId);
                        },
                        this.abortController.signal,
                        this.sessionId,
                        this.getSelectedModel(),
                        this.selectedReasoningEffort,
                        undefined,
                        undefined,
                        undefined,
                        userEntryId,
                        SessionStore.getRevision(this.sessionId));
                }
            });
        });
    }

    private initNavRail(wrap: HTMLElement) {
        this.navRail = document.createElement("div");
        this.navRail.className = "agent-chat__nav-rail";

        this.navRail.addEventListener("mouseenter", () => {
            this.navExpandTimer = window.setTimeout(() => {
                this.navRail.classList.add("agent-chat__nav-rail--expanded");
            }, 200);
        });
        this.navRail.addEventListener("mouseleave", () => {
            clearTimeout(this.navExpandTimer);
            this.navRail.classList.remove("agent-chat__nav-rail--expanded");
        });
        this.navRail.addEventListener("click", (e: MouseEvent) => {
            const marker = (e.target as HTMLElement).closest(".agent-chat__nav-rail-marker") as HTMLElement;
            if (!marker) {
                return;
            }
            this.jumpToMessage(marker.dataset.messageId || "");
        });

        wrap.appendChild(this.navRail);
    }

    private rebuildNavMarkers() {
        this.navRail.innerHTML = "";
        const userEntries = this.entries.filter((entry): entry is UserEntry => entry.type === "user");
        if (userEntries.length === 0) {
            return;
        }

        const gap = Math.max(0.5, Math.min(3, 40 / userEntries.length));
        this.navRail.style.setProperty("--nav-gap", gap + "px");

        for (const entry of userEntries) {
            const marker = document.createElement("div");
            marker.className = "agent-chat__nav-rail-marker ariaLabel";
            marker.dataset.messageId = entry.id || "";
            marker.setAttribute("data-position", "west");
            marker.setAttribute("aria-label", escapeAriaLabel(escapeHtml(entry.content)));
            marker.textContent = entry.content.slice(0, 120);
            this.navRail.appendChild(marker);
        }
        this.updateActiveMarker();
    }

    private updateActiveMarker() {
        const userMsgs = this.messagesContainer.querySelectorAll(".agent-chat__msg--user[data-message-id]");
        if (userMsgs.length === 0) {
            return;
        }
        const threshold = this.messagesContainer.scrollTop + 50;
        let activeId = "";
        for (let i = 0; i < userMsgs.length; i++) {
            if ((userMsgs[i] as HTMLElement).offsetTop <= threshold) {
                activeId = userMsgs[i].getAttribute("data-message-id") || "";
            } else {
                break;
            }
        }
        if (!activeId && userMsgs.length > 0) {
            activeId = userMsgs[0].getAttribute("data-message-id") || "";
        }
        const markers = this.navRail.children;
        for (let i = 0; i < markers.length; i++) {
            markers[i].classList.toggle("agent-chat__nav-rail-marker--active",
                markers[i].getAttribute("data-message-id") === activeId);
        }
    }

    private jumpToMessage(messageId: string) {
        if (!messageId) {
            return;
        }
        const el = this.messagesContainer.querySelector('[data-message-id="' + messageId + '"]') as HTMLElement;
        if (!el) {
            return;
        }
        el.scrollIntoView({behavior: "smooth", block: "center"});
        el.classList.add("agent-chat__msg--jumped");
        setTimeout(() => {
            el.classList.remove("agent-chat__msg--jumped");
        }, 1500);
    }

    private bindEvents() {
        // Hovering the tokens number at the bottom pops up the breakdown panel.
        // Only bind mouseenter/mouseleave on devices that support hover (on mobile, a tap
        // synthesizes mouse events, causing a flicker); on devices without hover support (mobile),
        // toggle with click instead.
        const supportsHover = window.matchMedia("(hover: hover)").matches;
        if (supportsHover) {
            this.tokenDisplayEl.addEventListener("mouseenter", () => {
                window.clearTimeout(this.tokenPopupHideTimer);
                this.tokenPopupShowTimer = window.setTimeout(() => {
                    this.showTokenBreakdownPopup();
                }, 200);
            });
            this.tokenDisplayEl.addEventListener("mouseleave", () => {
                window.clearTimeout(this.tokenPopupShowTimer);
                this.tokenPopupHideTimer = window.setTimeout(() => {
                    this.closeTokenBreakdownPopup();
                }, 300);
            });
        }
        // All devices: clicking toggles the popup. On hover-capable devices, stopPropagation
        // prevents the click from bubbling to document's outside-click handler, avoiding the
        // counter-intuitive case where clicking right after a hover reveal immediately closes it.
        this.tokenDisplayEl.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
            if (this.tokenPopup) {
                this.closeTokenBreakdownPopup();
            } else {
                this.showTokenBreakdownPopup();
            }
        });
        this.sendBtn.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
            this.sendMessage();
        });
        this.stopBtn.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
            void this.stopGeneration();
        });
        this.newSessionBtn.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
            setPanelFocus(this.parent.panelElement);
            this.createSession();
        });
        this.sessionMenuBtn.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
            setPanelFocus(this.parent.panelElement);
            this.sessionPanel.toggle();
        });

        this.parent.panelElement.addEventListener("click", (e: MouseEvent) => {
            setPanelFocus(this.parent.panelElement);
            const t = e.target as HTMLElement;
            let target = t;
            while (target && !target.isEqualNode(this.parent.panelElement)) {
                if (target.classList.contains("block__icon")) {
                    const type = target.getAttribute("data-type");
                    if (type === "min") {
                        e.stopPropagation();
                        getDockByType("agentChat").toggleModel("agentChat", false, true);
                        return;
                    }
                    break;
                }
                target = target.parentElement;
            }
            if (t.closest(".block__icons")) {
                return;
            }
            if (t.closest(".agent-chat__msg")) {
                return;
            }
            if (t.closest(".agent-chat__header")) {
                return;
            }
            if (t.closest(".agent-session-popup")) {
                return;
            }
            if (t.closest(".b3-select")) {
                return;
            }
            if (this.composer) {
                this.composer.focus();
            }
        });
        this.scrollBottomBtn.addEventListener("click", () => {
            this.scrollToBottom(true, true);
        });
    }

    private async initSessions() {
        // Always land on a new-session view (welcome page) at startup; do not auto-load the last
        // session's content.
        // Past sessions can still be viewed by switching to them from the session panel.
        await SessionStore.init();
        this.sessionId = SessionStore.newSessionId();
        this.sessionCreatedAt = Date.now();
        this.sessionTitle = this.defaultTitle;
        this.pendingSessionTitle = null;
        this.entries = [];
        this.showWelcome();
        this.scrollToBottom(true);
    }

    private async saveSession(commitTurnID?: string): Promise<AgentSession | null> {
        if (this.entries.length === 0) {
            return null;
        }
        const sessionID = this.sessionId;
        const recoveryTurnID = this.recoveryCommitTurnIDs.get(sessionID);
        const turnID = commitTurnID || recoveryTurnID;
        const pendingTitle = this.pendingSessionTitle;
        const session: AgentSession = {
            id: sessionID,
            title: this.sessionTitle,
            titled: this.hasTitled,
            entries: JSON.parse(JSON.stringify(this.entries.concat(this.pendingConfirms))) as AgentSession["entries"],
            contextTokens: this.contextTokens,
            contextTokenBreakdown: this.contextTokenBreakdown,
            contextCachedTokens: this.contextCachedTokens,
            contextLimit: this.contextLimit,
            createdAt: this.sessionCreatedAt,
            updatedAt: Date.now(),
            messageHistory: this.composer?.getHistory() || [],
            model: this.getSelectedModel(),
        };
        if (turnID) {
            session.commitTurnID = turnID;
        }
        const result = await SessionStore.save(session);
        if (turnID && this.recoveryCommitTurnIDs.get(sessionID) === turnID) {
            this.recoveryCommitTurnIDs.delete(sessionID);
        }
        if (turnID) {
            this.pendingRecoverySessionIDs.delete(sessionID);
        }
        if (this.sessionId !== sessionID) {
            return result.session ?? null;
        }
        if (pendingTitle !== null && pendingTitle === session.title && this.pendingSessionTitle === pendingTitle) {
            this.pendingSessionTitle = null;
        }
        if (turnID && this.currentTurnID === turnID) {
            this.currentTurnID = "";
        }
        if (this.pendingSessionTitle !== null && !this.isStreaming && !this.currentTurnID) {
            await this.saveSession();
        }
        return result.session ?? null;
    }

    // Handle cross-instance session change notifications pushed over ws. Core sequencing:
    // - streamStart: another instance started streaming. Immediately pull once from disk (the
    //   initiator already persisted the user message when sending), so this round's new user
    //   message becomes visible as soon as possible, then lock into a placeholder showing "AI
    //   reply generating".
    // - update: the session was saved, or an uncommitted runtime entered a recoverable state.
    //   Always read the backend's authoritative view each time; a save mid-stream only contains
    //   completed history and interaction cards, never treating a half-finished assistant text as
    //   the final result.
    // - streamEnd: the backend's eventCh closed (stream ended); only releases the placeholder lock.
    //   Committed content is synced by the update from saveSession, and uncommitted content is
    //   synced by the recovery update the backend sends right after.
    // - delete: clear the view if the current session was deleted.
    private onWsMessage(data: IWebSocketData) {
        if (!data || data.cmd !== "agentSessionChanged") {
            return;
        }
        const payload = data.data as { sessionID: string; action: string } | undefined;
        if (!payload) {
            return;
        }
        // Every change refreshes the session list (title/time/additions/removals).
        this.sessionPanel?.refresh();
        // Only handle the current session; changes to other sessions only show up in the list
        // refresh.
        if (payload.sessionID !== this.sessionId) {
            return;
        }
        // Ignore while the initiator itself is streaming (it renders itself via SSE).
        if (this.isStreaming) {
            return;
        }
        switch (payload.action) {
            case "streamStart":
                // Mark that another instance is streaming; reloadFromDisk keeps the placeholder bar
                // based on this after redrawing.
                this.mirrorLocked = true;
                // Pull once immediately: the initiator already called saveSession to write the user
                // message when sending, so this round's new message becomes visible as soon as
                // possible.
                void this.reloadFromDisk();
                break;
            case "streamEnd":
                // Stream ended: release the placeholder lock and remove the placeholder bar. Do not
                // redraw here — the full content is driven by the subsequent update broadcast.
                this.mirrorLocked = false;
                this.removeMirrorPlaceholder();
                this.restorePendingEditDraft();
                break;
            case "update":
                void this.reloadFromDisk().then(() => {
                    if (this.pendingRecoverySessionIDs.has(payload.sessionID)) {
                        void this.recoverInterruptedTurn(payload.sessionID, this.currentTurnID);
                    }
                    if (!this.mirrorLocked) {
                        this.restorePendingEditDraft();
                    }
                });
                break;
            case "delete":
                this.mirrorLocked = false;
                this.handleCurrentSessionDeleted();
                break;
        }
    }

    // Show a read-only "another instance is chatting..." placeholder bar. Does not enter the
    // setStreaming state (does not toggle the stop button or gray out the composer).
    private showMirrorPlaceholder() {
        if (this.mirrorPlaceholderEl) {
            return;
        }
        this.removeMirrorPlaceholder();
        const L = window.siyuan.languages;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--mirror";
        el.innerHTML = '<div class="agent-chat__body agent-chat__body--mirror">' +
            '<span class="agent-chat__mirror-spinner"></span>' +
            "<span>" + escapeHtml(L.agentMirrorStreaming || "Another instance is chatting...") + "</span>" +
            "</div>";
        this.messagesContainer.appendChild(el);
        this.mirrorPlaceholderEl = el;
        this.scrollToBottom();
    }

    private removeMirrorPlaceholder() {
        if (this.mirrorPlaceholderEl) {
            this.mirrorPlaceholderEl.remove();
            this.mirrorPlaceholderEl = null;
        }
    }

    // On the mirror side, pull the whole session's authoritative data from disk and redraw. Only
    // clear and redraw when entries actually changed, to avoid pointless jumps.
    private async reloadFromDisk(forceRender = false) {
        const targetSessionId = this.sessionId;
        const session = await SessionStore.load(targetSessionId);
        // The user may have switched sessions during the await; discard stale results.
        if (targetSessionId !== this.sessionId) {
            return;
        }
        if (!session) {
            return;
        }
        const newEntries = this.buildEntriesFromSession(session);
        if (!forceRender && this.entriesEqual(newEntries, this.entries)) {
            // Content unchanged, only update metadata (title, etc.).
            this.updateMetaFromSession(session);
            return;
        }
        const atBottom = this.isScrolledToBottom();
        const savedScroll = this.messagesContainer.scrollTop;
        if (forceRender) {
            this.currentAIElement = null;
            this.observeStickTarget(null);
            this.currentAssistantEntryId = "";
            this.currentContent = "";
            this.fullContent = "";
            this.currentToolCalls = [];
            this.pendingConfirms = [];
            this.currentThinkingSteps = [];
            this.currentThinkingEntryId = "";
            this.currentThinkingStepContent = "";
            this.currentThinkingText = "";
            this.currentThinkingReasoning = "";
            this.currentThinkingReasoningContent = "";
            this.currentThinkingDuration = 0;
            this.lastStepToolCount = 0;
            this.renderedToolNames = {};
            this.hasInterveningCard = false;
        }
        this.entries = newEntries;
        this.updateMetaFromSession(session);
        this.messagesContainer.innerHTML = "";
        this.renderLoadedSession(session);
        this.rebuildNavMarkers();
        if (atBottom) {
            this.scrollToBottom(true);
        } else {
            this.messagesContainer.scrollTop = savedScroll;
        }
        // Redrawing clears the DOM (including the placeholder bar); if still in another instance's
        // streaming state, show the placeholder bar again.
        if (this.mirrorLocked) {
            this.showMirrorPlaceholder();
        } else {
            this.removeMirrorPlaceholder();
        }
    }

    private async recoverInterruptedTurn(sessionID: string, turnID = "") {
        this.pendingRecoverySessionIDs.add(sessionID);
        if (this.recoveryInFlightSessionIDs.has(sessionID)) {
            return;
        }
        this.recoveryInFlightSessionIDs.add(sessionID);
        const retryDelays = [100, 200, 400, 800, 1600, 3200];
        try {
            for (const delay of retryDelays) {
                await new Promise((resolve) => window.setTimeout(resolve, delay));
                if (this.sessionId !== sessionID || this.isStreaming) {
                    return;
                }
                let session: AgentSession | null;
                try {
                    session = await SessionStore.load(sessionID);
                } catch (e) {
                    console.error("recover interrupted agent turn failed:", e);
                    continue;
                }
                if (!session?.recoveryTurnID) {
                    if (session && !session.agentRunning) {
                        this.pendingRecoverySessionIDs.delete(sessionID);
                        if (!turnID || this.currentTurnID === turnID) {
                            this.currentTurnID = "";
                        }
                        return;
                    }
                    continue;
                }
                if (turnID && session.recoveryTurnID !== turnID) {
                    continue;
                }
                try {
                    await this.reloadFromDisk(true);
                    if (this.sessionId !== sessionID) {
                        return;
                    }
                    if (this.recoveryCommitTurnIDs.get(sessionID) !== session.recoveryTurnID) {
                        continue;
                    }
                    this.currentTurnID = "";
                    await this.saveSession();
                    this.pendingRecoverySessionIDs.delete(sessionID);
                    return;
                } catch (e) {
                    console.error("commit recovered agent turn failed:", e);
                }
            }
        } finally {
            this.recoveryInFlightSessionIDs.delete(sessionID);
        }
    }

    private async prepareForNewTurn(): Promise<boolean> {
        const sessionID = this.sessionId;
        if (this.pendingRecoverySessionIDs.has(sessionID) && !this.recoveryCommitTurnIDs.has(sessionID)) {
            void this.recoverInterruptedTurn(sessionID, this.currentTurnID);
            const L = window.siyuan.languages;
            showMessage(L.agentChatBusy || "This session is busy in another instance", 3000);
            return false;
        }
        if (!this.recoveryCommitTurnIDs.has(sessionID)) {
            return true;
        }
        try {
            await this.saveSession();
            return this.sessionId === sessionID;
        } catch (e) {
            await this.reloadFromDisk(true);
            return false;
        }
    }

    // Update metadata such as title/timestamp/token counts/model from the session, without
    // touching entries or the DOM.
    private updateMetaFromSession(session: AgentSession) {
        this.sessionTitle = this.pendingSessionTitle || session.title || this.defaultTitle;
        this.hasTitled = session.titled !== false;
        this.sessionCreatedAt = session.createdAt || this.sessionCreatedAt;
        this.contextTokens = session.contextTokens ?? 0;
        this.contextTokenBreakdown = session.contextTokenBreakdown ?? {};
        this.contextCachedTokens = session.contextCachedTokens ?? 0;
        this.contextLimit = session.contextLimit ?? 0;
        if (session.recoveryTurnID) {
            this.recoveryCommitTurnIDs.set(session.id, session.recoveryTurnID);
        } else {
            this.recoveryCommitTurnIDs.delete(session.id);
        }
        if (session.model) {
            this.applySessionModelIfValid(session.model);
        }
        this.titleElement.textContent = this.sessionTitle;
        this.updateTokenDisplay();
    }

    // Shallow-compare whether two entries arrays are equal (used to decide whether a redraw is
    // needed). Compares via JSON serialization, simple and reliable.
    private entriesEqual(a: SessionEntry[], b: SessionEntry[]): boolean {
        if (a === b) {
            return true;
        }
        if (a.length !== b.length) {
            return false;
        }
        return JSON.stringify(a) === JSON.stringify(b);
    }

    private isScrolledToBottom(): boolean {
        const {scrollTop, scrollHeight, clientHeight} = this.messagesContainer;
        return scrollHeight - scrollTop - clientHeight <= 60;
    }

    // When the current session is deleted by another instance, clear it back to the welcome page.
    // Does not call saveSession (the session no longer exists on disk).
    private handleCurrentSessionDeleted() {
        this.pendingEditDraft = null;
        const deletedSessionID = this.sessionId;
        this.removeMirrorPlaceholder();
        this.entries = [];
        this.sessionId = SessionStore.newSessionId();
        this.currentTurnID = "";
        this.sessionCreatedAt = Date.now();
        this.sessionTitle = this.defaultTitle;
        this.pendingSessionTitle = null;
        this.pendingRecoverySessionIDs.delete(deletedSessionID);
        this.recoveryCommitTurnIDs.delete(deletedSessionID);
        this.hasTitled = false;
        this.currentAIElement = null;
        this.currentContent = "";
        this.fullContent = "";
        this.currentToolCalls = [];
        this.pendingConfirms = [];
        this.messagesContainer.innerHTML = "";
        this.rebuildNavMarkers();
        this.titleElement.textContent = this.defaultTitle;
        this.showWelcome();
        this.scrollToBottom(true);
    }

    private async switchSession(id: string) {
        this.pendingEditDraft = null;
        const previousSessionID = this.sessionId;
        const hadActiveTurn = this.isStreaming || !!this.currentTurnID;
        if (hadActiveTurn) {
            this.pendingRecoverySessionIDs.add(previousSessionID);
            if (this.abortController) {
                this.abortController.abort();
                this.abortController = null;
            }
        }
        this.setStreaming(false);
        this.mirrorLocked = false;
        this.removeMirrorPlaceholder();
        this.finishActiveThinking();
        this.flushThinkingStep();
        if (!hadActiveTurn && !this.pendingRecoverySessionIDs.has(this.sessionId)) {
            await this.saveSession();
        }
        const session = await SessionStore.load(id);
        if (!session) {
            return;
        }
        this.sessionId = session.id;
        this.currentTurnID = "";
        if (session.recoveryTurnID) {
            this.recoveryCommitTurnIDs.set(session.id, session.recoveryTurnID);
        } else {
            this.recoveryCommitTurnIDs.delete(session.id);
        }
        if (this.composer) {
            this.composer.clearHistory();
            this.composer.restoreHistory(session.messageHistory || []);
        }
        this.sessionCreatedAt = session.createdAt || Date.now();
        this.sessionTitle = session.title;
        this.pendingSessionTitle = null;
        this.titleElement.textContent = session.title || this.defaultTitle;
        this.entries = this.buildEntriesFromSession(session);
        this.hasTitled = session.titled !== false;
        this.currentAIElement = null;
        this.currentContent = "";
        this.fullContent = "";
        this.contextTokens = session.contextTokens ?? 0;
        this.contextTokenBreakdown = session.contextTokenBreakdown ?? {};
        this.contextCachedTokens = session.contextCachedTokens ?? 0;
        this.contextLimit = session.contextLimit ?? 0;
        if (session.model) {
            this.applySessionModelIfValid(session.model);
        }
        if (this.tokenDisplayEl) {
            this.updateTokenDisplay();
        }
        this.messagesContainer.classList.add("agent-chat__messages--switching");
        this.messagesContainer.addEventListener("transitionend", () => {
            this.messagesContainer.innerHTML = "";
            this.titleElement.textContent = session.title;
            this.renderLoadedSession(session);
            this.rebuildNavMarkers();
            // Restore this session's last scroll position; a new session (no record) defaults to
            // the bottom.
            if (this.scrollBottomBySession.has(session.id)) {
                this.restoreScrollToBottom(this.scrollBottomBySession.get(session.id) ?? 0);
            } else {
                this.scrollToBottom(true);
            }
            this.messagesContainer.classList.remove("agent-chat__messages--switching");
            if (this.pendingRecoverySessionIDs.has(session.id)) {
                void this.recoverInterruptedTurn(session.id);
            }
        }, {once: true});
    }

    private appendPersistedAssistant(content: string, timestamp?: number, entryId?: string) {
        if (!content || !content.trim()) {
            return;
        }
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--ai";
        if (entryId) {
            el.setAttribute("data-message-id", entryId);
        }
        el.innerHTML = '<div class="agent-chat__body b3-typography">' + (this.lute.ProtylePreviewStr("", content) || escapeHtml(content)) + "</div>";
        this.messagesContainer.appendChild(el);
        postRender(el, this.app);
        this.addCopyButton(el, content, timestamp);
    }

    private appendPersistedToolCalls(content: string, toolCalls: Array<{
        name: string;
        arguments: Record<string, unknown>;
        result?: string
    }>, timestamp?: number, entryId?: string) {
        let hasRendered = false;
        for (let i = 0; i < toolCalls.length; i++) {
            const tc = toolCalls[i];
            if (tc.result && tc.name === "todo_write") {
                const rel = document.createElement("div");
                rel.className = "agent-chat__msg agent-chat__msg--tool";
                // The todo card is a supplementary display for the assistant entry and does not
                // hold its own entryId (entryId belongs to the subsequent AI message element),
                // avoiding multiple todos sharing the same id.
                rel.innerHTML = renderTodoList(tc.result);
                this.messagesContainer.appendChild(rel);
                hasRendered = true;
            }
        }
        if (content && content.trim()) {
            this.appendPersistedAssistant(content, timestamp, entryId);
            hasRendered = true;
        }
        if (!hasRendered) {
            return;
        }
    }

    private appendPersistedConfirm(entry: {
        id?: string;
        name: string;
        args: Record<string, unknown>;
        confirmID: string;
        effects?: IToolEffects;
        status?: string
    }) {
        const L = window.siyuan.languages;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--confirm agent-chat__msg--confirmed";
        if (entry.id) {
            el.setAttribute("data-message-id", entry.id);
        }
        const argsStr = JSON.stringify(entry.args, null, 2);
        const desc = (L.agentConfirmDesc || "Agent: {category} operation").replace("{category}", escapeHtml(this.toolCategory(entry.name)));
        let statusLabel = "";
        if (entry.status === "approved") {
            statusLabel = L.agentConfirmApprove || "Approved";
        } else if (entry.status === "rejected") {
            statusLabel = L.agentConfirmReject || "Rejected";
        } else if (entry.status === "always") {
            statusLabel = L.agentConfirmAlways || "Session Allow";
        } else {
            // pending (persisted because the user switched away or an error occurred before
            // acting): no longer interactive after a reload, so at least show a status hint to
            // avoid ending up as a dead card with no button and no text.
            statusLabel = L.agentConfirmPending || "Pending";
        }
        el.innerHTML = '<div class="agent-chat__confirm-card">' +
            '<div class="agent-chat__confirm-header"><svg class="agent-chat__confirm-icon"><use xlink:href="#iconInfo"></use></svg> ' + desc + "</div>" +
            this.renderConfirmEffects(entry.effects) +
            '<pre class="agent-chat__confirm-args">' + escapeHtml(argsStr) + "</pre>" +
            (statusLabel ? '<div class="agent-chat__confirm-actions"><span class="agent-chat__confirm-done">' + statusLabel + "</span></div>" : "") +
            "</div>";
        this.messagesContainer.appendChild(el);
    }

    // Slim down toolCalls before persisting: the question tool's full questions argument is already
    // stored by a separate question entry, so the toolCalls in the assistant entry only need to
    // keep the tool name and result (for restoring LLM context), avoiding duplicate storage.
    private slimToolCallsForPersistence(toolCalls: Array<{
        name: string;
        arguments: Record<string, unknown>;
        result?: string
    }>): Array<{ name: string; arguments: Record<string, unknown>; result?: string }> {
        return toolCalls.map(tc => {
            if (tc.name === "question" && tc.arguments && tc.arguments.questions) {
                const slim = {...tc};
                const slimArgs = {...tc.arguments};
                delete slimArgs.questions;
                slim.arguments = slimArgs;
                return slim;
            }
            return tc;
        });
    }

    private appendPersistedQuestion(entry: {
        id?: string;
        questionID: string;
        questions: Array<Record<string, unknown>>;
        status?: string;
        answers?: string[];
    }) {
        const L = window.siyuan.languages;
        const el = document.createElement("div");
        // A question is no longer interactive after a reload (the backend has timed out or the
        // session has switched), so uniformly display it as confirmed.
        el.className = "agent-chat__msg agent-chat__msg--question agent-chat__msg--confirmed";
        if (entry.id) {
            el.setAttribute("data-message-id", entry.id);
        }
        el.innerHTML = renderQuestionCardHTML(entry.questions, entry.questionID);
        // Replace the submit button area with status text, matching the appearance after a live
        // submission.
        const submit = el.querySelector(".agent-chat__question-submit") as HTMLElement;
        if (submit) {
            const submitted = entry.status === "submitted";
            submit.innerHTML = '<span class="agent-chat__confirm-done">' +
                (submitted ? (L.agentQuestionSubmitted || "Submitted") : (L.agentQuestionPending || "Awaiting answer")) +
                "</span>";
        }
        // Disable all inputs so the user does not mistakenly think they can still submit.
        el.querySelectorAll("input").forEach((inp) => {
            (inp as HTMLInputElement).disabled = true;
        });
        // Restore the previously submitted selection state (answers stores option values).
        if (entry.answers && entry.answers.length > 0) {
            el.querySelectorAll("input[type=radio], input[type=checkbox]").forEach((inp) => {
                (inp as HTMLInputElement).checked = entry.answers!.includes((inp as HTMLInputElement).value);
            });
            const customInput = el.querySelector(".agent-chat__question-custom") as HTMLInputElement | null;
            if (customInput) {
                const customAnswer = entry.answers.find(a => el.querySelector('input[value="' + a + '"]') === null);
                if (customAnswer) {
                    customInput.value = customAnswer;
                }
            }
        }
        this.messagesContainer.appendChild(el);
    }

    private renderLoadedSession(session: AgentSession) {
        this.editingUserEntryID = "";
        for (let i = 0; i < session.entries.length; i++) {
            const entry = session.entries[i];
            const entryId = (entry as { id?: string }).id;
            switch (entry.type) {
                case "user":
                    this.appendUserMessage((entry as UserEntry).content, (entry as UserEntry).timestamp, entryId,
                        (entry as UserEntry).blockHTML);
                    break;
                case "thinking":
                    if (entry.steps && entry.steps.length > 0) {
                        // Compatibility with old data: an old step may contain text ("Thought for
                        // Xs") and toolCalls ({name, result}); normalize it here into the new format
                        // (toolNames + entry.duration).
                        const rawEntry = entry as {
                            steps: Array<{
                                reasoning: string;
                                reasoningContent?: string;
                                toolNames?: string[];
                                toolCalls?: Array<{ name: string; result?: string }>;
                                text?: string;
                                content?: string
                            }>;
                            duration?: number
                        };
                        const normSteps = rawEntry.steps.map(s => ({
                            reasoning: s.reasoning || "",
                            reasoningContent: s.reasoningContent || "",
                            toolNames: (s.toolNames && s.toolNames.length > 0)
                                ? s.toolNames
                                : (s.toolCalls ? s.toolCalls.map(t => t.name) : undefined),
                            content: s.content,
                        }));
                        // entry.duration takes priority; otherwise try to extract it from the last
                        // step.text (old format).
                        let dur: number | undefined = rawEntry.duration;
                        if (dur === undefined) {
                            const lastText = rawEntry.steps[rawEntry.steps.length - 1]?.text;
                            if (lastText) {
                                const m = lastText.match(/([\d.]+)\s*s/i);
                                if (m) {
                                    dur = parseFloat(m[1]);
                                }
                            }
                        }
                        this.renderMergedThinkingCard(normSteps, entryId, dur);
                    }
                    break;
                case "assistant": {
                    const a = entry as {
                        content: string;
                        toolCalls?: Array<{ name: string; arguments: Record<string, unknown>; result?: string }>;
                        timestamp?: number
                    };
                    if (a.toolCalls && a.toolCalls.length > 0) {
                        this.appendPersistedToolCalls(a.content, a.toolCalls, a.timestamp, entryId);
                    } else {
                        this.appendPersistedAssistant(a.content, a.timestamp, entryId);
                    }
                    break;
                }
                case "confirm":
                    this.appendPersistedConfirm(entry as unknown as {
                        id?: string;
                        name: string;
                        args: Record<string, unknown>;
                        confirmID: string;
                        status?: string
                    });
                    break;
                case "question":
                    this.appendPersistedQuestion(entry as unknown as {
                        id?: string;
                        questionID: string;
                        questions: Array<Record<string, unknown>>;
                        status?: string;
                        answers?: string[];
                    });
                    break;
                case "snapshot":
                    this.appendSnapshotInfo((entry as { snapshotID: string }).snapshotID, entryId);
                    break;
                case "rollback":
                    this.appendRollbackInfo((entry as { snapshotID: string }).snapshotID, entryId);
                    break;
            }
        }
    }

    private buildEntriesFromSession(session: AgentSession): SessionEntry[] {
        if (session.messages && session.messages.length > 0) {
            const entriesLen = session.entries ? session.entries.length : 0;
            if (session.messages.length > entriesLen) {
                const entries: SessionEntry[] = [];
                for (let i = 0; i < session.messages.length; i++) {
                    const msg = session.messages[i];
                    if (msg.role === "user") {
                        entries.push({id: SessionStore.newSessionId(), type: "user", content: msg.content});
                    } else if (msg.role === "assistant") {
                        entries.push({
                            id: SessionStore.newSessionId(),
                            type: "assistant",
                            content: msg.content,
                            toolCalls: msg.toolCalls ? msg.toolCalls.map(tc => ({
                                name: tc.name,
                                arguments: tc.arguments || {},
                                result: tc.result,
                            })) : undefined,
                        });
                    }
                }
                return entries;
            }
        }
        if (session.entries && session.entries.length > 0) {
            return session.entries as any as SessionEntry[];
        }
        return [];
    }

    private async createSession() {
        this.pendingEditDraft = null;
        const previousSessionID = this.sessionId;
        const hadActiveTurn = this.isStreaming || !!this.currentTurnID;
        if (hadActiveTurn) {
            this.pendingRecoverySessionIDs.add(previousSessionID);
        }
        if (this.abortController) {
            this.abortController.abort();
            this.abortController = null;
        }
        this.setStreaming(false);
        this.mirrorLocked = false;
        this.removeMirrorPlaceholder();
        this.finishActiveThinking();
        this.flushThinkingStep();
        if (!hadActiveTurn && !this.pendingRecoverySessionIDs.has(this.sessionId)) {
            await this.saveSession();
        }
        this.sessionId = SessionStore.newSessionId();
        this.currentTurnID = "";
        this.sessionCreatedAt = Date.now();
        if (this.composer) {
            this.composer.clearHistory();
        }
        this.sessionTitle = this.defaultTitle;
        this.pendingSessionTitle = null;
        this.entries = [];
        this.hasTitled = false;
        this.currentAIElement = null;
        this.currentContent = "";
        this.fullContent = "";
        this.contextTokens = 0;
        this.contextTokenBreakdown = {};
        this.contextCachedTokens = 0;
        this.contextLimit = 0;
        this.currentToolCalls = [];
        this.lastStepToolCount = 0;
        this.renderedToolNames = {};
        this.hasInterveningCard = false;
        if (this.tokenDisplayEl) {
            this.tokenDisplayEl.classList.add("fn__none");
        }
        this.messagesContainer.innerHTML = "";
        this.currentThinkingSteps = [];
        this.currentThinkingStepContent = "";
        this.pendingConfirms = [];
        this.rebuildNavMarkers();
        this.titleElement.textContent = this.defaultTitle;
        if (this.composer) {
            this.composer.clear();
        }
        if (this.composer) {
            this.composer.focus();
        }
        // clear() is a programmatic clear that does not trigger the native input event, so the
        // send button must be refreshed explicitly (empty input -> disabled).
        this.updateSendButtonState();
        this.showWelcome();
        this.scrollToBottom(true);
    }

    private async deleteSession(id: string) {
        if (id === this.sessionId && (this.isStreaming || !!this.currentTurnID ||
            this.pendingRecoverySessionIDs.has(id))) {
            const L = window.siyuan.languages;
            showMessage(L.agentChatBusy || "This session is busy in another instance", 3000);
            return;
        }
        this.scrollBottomBySession.delete(id);
        const wasCurrent = id === this.sessionId;
        if (wasCurrent) {
            const result = await SessionStore.list({page: 1, pageSize: 2});
            const list = result.sessions.filter(s => s.id !== id);
            if (list.length > 0) {
                await this.switchSession(list[0].id);
                await SessionStore.remove(id);
            } else {
                this.entries = [];
                this.sessionId = SessionStore.newSessionId();
                await this.createSession();
                await SessionStore.remove(id);
            }
        } else {
            await SessionStore.remove(id);
        }
        this.pendingRecoverySessionIDs.delete(id);
        this.recoveryCommitTurnIDs.delete(id);
    }

    private async sendMessage() {
        if (!this.composer) {
            return;
        }
        const sendData = this.composer.getSendData();
        const text = sendData.text;
        const blockHTML = sendData.blockHTML;
        const refs = sendData.references;
        const editorContext = this.captureEditorContext();
        const pluginActions = this.getPluginActions();
        if (!text || this.isStreaming || this.modelOptions.length === 0) {
            return;
        }
        if (!await this.prepareForNewTurn()) {
            return;
        }

        this.setStreaming(true);
        this.clearThinking();
        this.hasInterveningCard = false;
        this.composer.clear();

        const userEntryId = SessionStore.newSessionId();
        this.entries.push({
            id: userEntryId,
            type: "user",
            content: text,
            blockHTML,
            references: refs.length > 0 ? refs : undefined,
            editorContext,
            timestamp: Date.now(),
        });
        if (this.entries.length === 1) {
            this.messagesContainer.innerHTML = "";
        }
        this.appendUserMessage(text, Date.now(), userEntryId, blockHTML);
        this.rebuildNavMarkers();
        this.tryGenerateTitle();
        if (this.composer) {
            this.composer.pushHistory(text);
        }
        try {
            await this.saveSession();
        } catch (e) {
            this.rollbackUserEntry(userEntryId);
            this.setStreaming(false);
            await this.reloadFromDisk();
            return;
        }

        this.requestStartTime = Date.now();
        this.currentThinkingDuration = 0;
        this.currentTurnID = "";

        this.abortController = new AbortController();
        const requestSessionId = this.sessionId;

        await fetchAgentSSE(
            text,
            window.siyuan.config.appearance.lang,
            refs,
            (event: ISSEResult) => {
                if (this.sessionId !== requestSessionId) {
                    return;
                }
                return this.handleSSEEvent(event);
            },
            (err: Error) => {
                if (this.sessionId !== requestSessionId) {
                    return;
                }
                // 409: this session is being chatted in by another instance (instance-level mutual
                // exclusion). Reload the authoritative disk state instead of entering streaming.
                if (err instanceof AgentHttpError && err.status === 409) {
                    this.handleConflictReject();
                    return;
                }
                return this.handleConfigError(err, userEntryId);
            },
            this.abortController.signal,
            this.sessionId,
            this.getSelectedModel(),
            this.selectedReasoningEffort,
            undefined,
            editorContext,
            pluginActions,
            userEntryId,
            SessionStore.getRevision(this.sessionId),
        );
    }

    // Being rejected by instance-level mutual exclusion (409) means another instance started first
    // right after this round's save. Disk may already have the other instance's new message by
    // now, so we can no longer roll back with the local snapshot; reload the authoritative session
    // directly, to avoid overwriting the other instance's data.
    private async handleConflictReject() {
        this.requestStartTime = 0;
        this.setStreaming(false);
        await this.reloadFromDisk(true);
        this.restorePendingEditDraft();
        const L = window.siyuan.languages;
        showMessage(L.agentChatBusy || "This session is busy in another instance", 3000);
    }

    private restorePendingEditDraft() {
        const draft = this.pendingEditDraft;
        if (!draft) {
            return;
        }
        const userEl = this.messagesContainer.querySelector(
            '.agent-chat__msg--user[data-message-id="' + draft.entryID + '"]') as HTMLElement | null;
        if (userEl) {
            this.beginEditUserMessage(draft.entryID, userEl, draft.content);
        }
    }

    // Capture a read-only editor snapshot at the time of sending, and inject the corresponding user
    // turn context.
    // Scan all editors, preferring one that is visible and contains selected blocks, to match what
    // the user means by "the blocks selected here".
    // If none is found, fall back in order to the editor holding the DOM selection, then the
    // most-recently-activated tab.
    private captureEditorContext(): IEditorContext | undefined {
        /// #if MOBILE
        const mobEditor = window.siyuan.mobile.editor || window.siyuan.mobile.popEditor;
        if (mobEditor?.protyle && !mobEditor.protyle.element.classList.contains("fn__none")) {
            return this.readEditorContext(mobEditor);
        }
        return undefined;
        /// #else
        const allEditor = getAllEditor();
        if (!allEditor || allEditor.length === 0) {
            return undefined;
        }
        const isEditable = (e: { protyle: { element: HTMLElement } }) =>
            !e.protyle.element.classList.contains("fn__none") &&
            e.protyle.element.closest(".layout__center") !== null;

        // Aggregate selected block IDs across ALL editors (user may have selected blocks
        // in one editor while a different one is "active").
        let allSelected: string[] = [];
        allEditor.forEach(e => {
            e.protyle?.wysiwyg?.element?.querySelectorAll("[data-node-id].protyle-wysiwyg--select")
                ?.forEach(el => {
                    const id = (el as HTMLElement).getAttribute("data-node-id");
                    if (id) {
                        allSelected.push(id);
                    }
                });
        });
        allSelected = Array.from(new Set(allSelected));

        // Candidate selection, in priority order:
        let candidate: {
            protyle: {
                block?: { id?: string; rootID?: string };
                wysiwyg?: { element?: HTMLElement };
                element: HTMLElement;
                model?: { parent?: { headElement?: HTMLElement } }
            }
        } | undefined;

        // 1) An editable editor that has its own selected blocks.
        candidate = allEditor.find(e => isEditable(e) &&
            !!e.protyle?.wysiwyg?.element?.querySelector(".protyle-wysiwyg--select"));
        // 2) The editor hosting the current DOM selection.
        if (!candidate) {
            const domSel = window.getSelection();
            const range = domSel && domSel.rangeCount > 0 ? domSel.getRangeAt(0) : null;
            if (range) {
                candidate = allEditor.find(e => e.protyle.element.contains(range.startContainer));
            }
        }
        // 3) The most-recently-activated focused document tab (data-activetime).
        if (!candidate) {
            let activeTime = 0;
            allEditor.forEach(e => {
                let head = e.protyle.model?.parent?.headElement;
                if (!head && e.protyle.element.getBoundingClientRect().height > 0) {
                    const tabBody = e.protyle.element.closest(".fn__flex-1[data-id]");
                    if (tabBody) {
                        head = document.querySelector(
                            `.layout-tab-bar .item[data-id="${tabBody.getAttribute("data-id")}"]`);
                    }
                }
                if (head && head.classList.contains("item--focus") &&
                    parseInt(head.dataset.activetime || "0") > activeTime) {
                    activeTime = parseInt(head.dataset.activetime || "0");
                    candidate = e;
                }
            });
        }
        // 4) Any visible (non-fn__none) editor.
        if (!candidate) {
            candidate = allEditor.find(e => !e.protyle.element.classList.contains("fn__none"));
        }

        const ctx = candidate ? this.readEditorContext(candidate) : undefined;
        // Even if no candidate editor was located, surface the globally-collected selections.
        if ((!ctx || !ctx.selectedBlockIDs || ctx.selectedBlockIDs.length === 0) && allSelected.length > 0) {
            const merged: IEditorContext = ctx ? {...ctx} : {};
            merged.selectedBlockIDs = allSelected;
            return merged;
        }
        return ctx;
        /// #endif
    }

    private getPluginActions() {
        return listActions()
            .filter(action => action.name.startsWith("plugin__") && action.description)
            .map(action => ({name: action.name, description: action.description as string}));
    }

    private readEditorContext(editor: {
        protyle: {
            block?: { id?: string; rootID?: string };
            wysiwyg?: { element?: HTMLElement };
            contentElement?: HTMLElement;
            notebookId?: string;
            title?: { editElement?: HTMLElement };
        };
    }): IEditorContext | undefined {
        const p = editor.protyle;
        if (!p) {
            return undefined;
        }
        const activeDocID = p.block?.rootID;
        const focusedBlockID = p.block?.id;
        const activeDocTitle = p.title?.editElement?.textContent?.trim() || undefined;
        const notebookID = p.notebookId || undefined;

        const selectedBlockIDs: string[] = [];
        p.wysiwyg?.element?.querySelectorAll("[data-node-id].protyle-wysiwyg--select")
            ?.forEach(el => {
                const id = (el as HTMLElement).getAttribute("data-node-id");
                if (id) {
                    selectedBlockIDs.push(id);
                }
            });

        // Visible blocks: top-level [data-node-id] children whose bounding rect intersects
        // the scroll container viewport. Long docs are lazily loaded, so wysiwyg.element's
        // children are already the loaded subset; this further narrows to what is on screen.
        const visibleBlockIDs: string[] = [];
        const scrollContainer = (p.contentElement || p.wysiwyg?.element?.parentElement as HTMLElement | undefined);
        if (scrollContainer && p.wysiwyg?.element) {
            const view = scrollContainer.getBoundingClientRect();
            const children = p.wysiwyg.element.children;
            for (let i = 0; i < children.length; i++) {
                const child = children[i] as HTMLElement;
                const id = child.getAttribute("data-node-id");
                if (!id) {
                    continue;
                }
                const rect = child.getBoundingClientRect();
                if (rect.height === 0) {
                    continue;
                }
                if (rect.bottom >= view.top && rect.top <= view.bottom) {
                    visibleBlockIDs.push(id);
                }
                if (visibleBlockIDs.length >= maxVisibleBlockIDs) {
                    break;
                }
            }
        }

        if (!activeDocID && !activeDocTitle && !notebookID &&
            !focusedBlockID && selectedBlockIDs.length === 0 && visibleBlockIDs.length === 0) {
            return undefined;
        }
        const ctx: IEditorContext = {};
        if (activeDocID) {
            ctx.activeDocID = activeDocID;
        }
        if (activeDocTitle) {
            ctx.activeDocTitle = activeDocTitle;
        }
        if (notebookID) {
            ctx.notebookID = notebookID;
        }
        if (focusedBlockID && focusedBlockID !== activeDocID) {
            ctx.focusedBlockID = focusedBlockID;
        }
        if (selectedBlockIDs.length > 0) {
            ctx.selectedBlockIDs = selectedBlockIDs;
        }
        if (visibleBlockIDs.length > 0) {
            ctx.visibleBlockIDs = visibleBlockIDs;
        }
        return ctx;
    }

    private async handleSSEEvent(event: ISSEResult) {
        try {
            switch (event.type) {
                case "turn":
                    this.currentTurnID = event.turnID;
                    break;
                case "content":
                    this.appendToken(event.token);
                    break;
                case "thinking":
                    this.appendThinking(event.reasoning);
                    break;
                case "tool_call":
                    this.currentToolCalls.push({name: event.name, arguments: event.arguments});
                    this.appendToolCall(event.name);
                    break;
                case "confirm":
                    this.setToolCallRunning(event.name, false);
                    this.appendConfirm(event.name, event.arguments, event.confirmID, event.effects);
                    break;
                case "tool_result":
                    {
                        const toolCall = this.currentToolCalls.find((item) => item.name === event.name && item.result === undefined);
                        if (toolCall) {
                            toolCall.result = event.result;
                        }
                    }
                    this.finishToolCall(event.name);
                    this.appendToolResult(event.name, event.result);
                    break;
                case "done":
                    this.currentTurnID = event.turnID || this.currentTurnID;
                    this.flushTokenUpdate();
                    await this.finishResponse();
                    break;
                case "usage":
                    this.appendUsage(event.lastPromptTokens, event.tokenBreakdown, event.cachedTokens, event.contextLimit);
                    break;
                case "error":
                    this.flushTokenUpdate();
                    this.requestStartTime = 0;
                    if (this.currentTurnID) {
                        // A server-side error is a terminal event: the runtime checkpoint and all
                        // tool result sends have already completed by this point, so the normal
                        // finish-up path can be reused — write the partial reply and tool calls into
                        // entries and commit this turn.
                        await this.finishResponse(false);
                        this.appendError(event.message);
                    } else {
                        // An error before the turn is established has no runtime to commit, so
                        // restore the authoritative disk state directly.
                        await this.handleError(new Error(event.message));
                    }
                    break;
                case "interrupted":
                    await this.handleError(new Error(event.message));
                    break;
                case "retry":
                    this.appendRetry(event.attempt, event.maxRetries);
                    break;
                case "question":
                    this.appendQuestion(event.questionID, event.arguments);
                    break;
                case "reasoning":
                    this.appendReasoning(event.token);
                    break;
                case "snapshot": {
                    const snapshotEntryId = SessionStore.newSessionId();
                    this.entries.push({id: snapshotEntryId, type: "snapshot", snapshotID: event.snapshotID});
                    this.appendSnapshotInfo(event.snapshotID, snapshotEntryId);
                }
                    break;
                case "frontend_tool_call":
                    this.handleFrontendToolCall(event.callID, event.arguments);
                    break;
            }
        } catch (e) {
            console.error("agent SSE event handler error:", e, event);
            if (this.abortController) {
                this.abortController.abort();
                this.abortController = null;
            }
            this.flushTokenUpdate();
            this.requestStartTime = 0;
            this.setStreaming(false);
            const sessionID = this.sessionId;
            const turnID = this.currentTurnID;
            try {
                await this.reloadFromDisk(true);
            } catch (reloadError) {
                console.error("reload agent session after event failure failed:", reloadError);
            }
            if (this.sessionId === sessionID) {
                this.appendError((e as Error).message);
                if (turnID) {
                    void this.recoverInterruptedTurn(sessionID, turnID);
                }
            }
        }
    }

    private async handleError(err: Error) {
        this.flushTokenUpdate();
        this.requestStartTime = 0;
        this.setStreaming(false);
        const sessionID = this.sessionId;
        const turnID = this.currentTurnID;
        try {
            await this.reloadFromDisk(true);
        } catch (reloadError) {
            console.error("reload agent session after stream failure failed:", reloadError);
        }
        if (this.sessionId === sessionID) {
            if (!turnID) {
                this.restorePendingEditDraft();
            }
            this.appendError(err.message);
            // A network disconnect does not mean the server-side turn has ended, so we must not
            // commit and clear the runtime. Merge the runtime only after the backend releases the
            // running instance, to avoid re-executing external calls whose results are unknown. The
            // turn event may also not have arrived yet before the disconnect.
            void this.recoverInterruptedTurn(sessionID, turnID);
        }
    }

    // Uniformly handle fetchAgentSSE's onError: if it is a "model/provider not configured" error,
    // render an actionable error card; otherwise fall back to a plain error card. userEntryId is
    // used to roll back the just-appended user message on "not configured" (to avoid leaving an
    // empty conversation).
    private async handleConfigError(err: Error, userEntryId?: string, restoreSession = false) {
        this.flushTokenUpdate();
        if (this.currentContent) {
            this.finalizeStreamingBody(this.currentContent, Date.now());
        }
        this.requestStartTime = 0;
        const configMsg = window.siyuan.languages._kernel[193] || "";
        const isConfigError = !!configMsg && err.message === configMsg;
        if (isConfigError) {
            if (restoreSession) {
                // Regeneration only truncates the frontend view before the agent establishes a
                // runtime; on a config error the original answer should be restored, and this
                // temporary truncated state must not be saved to session.json.
                await this.reloadFromDisk(true);
            } else {
                if (userEntryId) {
                    this.rollbackUserEntry(userEntryId);
                }
                if (this.entries.length === 0) {
                    await SessionStore.remove(this.sessionId);
                    this.sessionTitle = this.defaultTitle;
                    this.pendingSessionTitle = null;
                    this.hasTitled = false;
                    this.titleElement.textContent = this.defaultTitle;
                    void this.sessionPanel?.refresh();
                } else {
                    await this.saveSession();
                }
            }
            await this.appendConfigurableError(configMsg);
        } else {
            await this.handleError(err);
            return;
        }
        this.setStreaming(false);
        if (isConfigError && restoreSession) {
            this.restorePendingEditDraft();
        }
    }

    // Roll back the just-appended user entry and its DOM element (used to avoid leaving an empty
    // conversation on a "not configured" error).
    private rollbackUserEntry(userEntryId: string) {
        const idx = this.entries.findIndex(e => e.id === userEntryId);
        if (idx >= 0) {
            this.entries.splice(idx, 1);
        }
        const userEl = this.messagesContainer.querySelector('.agent-chat__msg--user[data-message-id="' + userEntryId + '"]');
        if (userEl) {
            userEl.remove();
        }
        this.rebuildNavMarkers();
    }

    private async appendConfigurableError(message: string) {
        this.finishActiveThinking();
        this.clearThinking();
        if (this.currentAIElement && !this.currentContent) {
            this.currentAIElement.remove();
        }
        this.currentAIElement = null;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--error";
        el.innerHTML = '<div class="agent-chat__body agent-chat__body--error">' +
            '<svg class="agent-chat__error-icon"><use xlink:href="#iconTriangleAlert"></use></svg>' +
            "<span>" + escapeHtml(message) + "</span>" +
            "</div>";
        this.messagesContainer.appendChild(el);
        this.scrollToBottom(true);
        this.flushThinkingStep();
    }

    private createUserMessage(text: string, timestamp?: number, entryId?: string, blockHTML?: string): HTMLElement {
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--user";
        if (entryId) {
            el.setAttribute("data-message-id", entryId);
        }
        const body = document.createElement("div");
        body.className = "agent-chat__body protyle-wysiwyg";
        body.setAttribute("contenteditable", "false");
        body.setAttribute("data-readonly", "true");
        body.innerHTML = blockHTML || this.lute.Md2BlockDOM(text);
        el.appendChild(body);
        let actionsHTML = '<div class="agent-chat__msg-actions">';
        if (timestamp) {
            actionsHTML += '<span class="agent-chat__msg-meta agent-chat__msg-time">' + this.formatMessageTime(timestamp) + "</span>";
        }
        actionsHTML += '<span class="block__icon block__icon--show ariaLabel agent-chat__user-copy" data-position="north" aria-label="' + window.siyuan.languages.copy + '"><svg><use xlink:href="#iconCopy"></use></svg></span>' +
            '<span class="block__icon block__icon--show ariaLabel agent-chat__user-edit" data-position="north" aria-label="' + window.siyuan.languages.edit + '"><svg><use xlink:href="#iconEdit"></use></svg></span>' +
            "</div>";
        el.insertAdjacentHTML("beforeend", actionsHTML);
        el.querySelector(".agent-chat__user-copy")?.addEventListener("click", (e) => {
            e.stopPropagation();
            navigator.clipboard.writeText(text).then(() => {
                showMessage(window.siyuan.languages.copied, 2000);
            }).catch(() => {
                showMessage(window.siyuan.languages.copied, 2000);
            });
        });
        const edit = (force = false) => {
            const selection = window.getSelection();
            const selectingMessageText = selection && !selection.isCollapsed && el.contains(selection.anchorNode);
            if (!entryId || this.isStreaming || this.mirrorLocked || (!force && selectingMessageText)) {
                return;
            }
            this.beginEditUserMessage(entryId, el);
        };
        el.querySelector(".agent-chat__user-edit")?.addEventListener("click", (e) => {
            e.stopPropagation();
            edit(true);
        });
        body.addEventListener("click", (event) => {
            const target = event.target as HTMLElement;
            if (!target.closest('[data-type~="a"], [data-type~="block-ref"], ' +
                '[data-type~="file-annotation-ref"], [data-type~="tag"], [data-subtype], a[href], img')) {
                edit();
            }
        });
        return el;
    }

    private renderUserMessage(el: HTMLElement) {
        const body = el.querySelector(".agent-chat__body") as HTMLElement;
        postRender(el, this.app);
        this.composer?.renderBlockHTML(body, () => {
            disabledWYSIWYG(body);
        });
        disabledWYSIWYG(body);
    }

    private appendUserMessage(text: string, timestamp?: number, entryId?: string, blockHTML?: string) {
        const el = this.createUserMessage(text, timestamp, entryId, blockHTML);
        this.messagesContainer.appendChild(el);
        this.renderUserMessage(el);
        this.scrollToBottom(true);
    }

    private beginEditUserMessage(entryID: string, el: HTMLElement, initialContent?: string) {
        if (this.editingUserEntryID || this.isStreaming || this.mirrorLocked) {
            return;
        }
        const entry = this.entries.find((item): item is UserEntry => item.type === "user" && item.id === entryID);
        if (!entry) {
            return;
        }
        this.editingUserEntryID = entryID;
        el.classList.add("agent-chat__msg--editing");
        const body = el.querySelector(".agent-chat__body") as HTMLElement;
        const actions = el.querySelector(".agent-chat__msg-actions") as HTMLElement;
        const textarea = document.createElement("textarea");
        textarea.className = "b3-text-field agent-chat__edit-textarea";
        textarea.value = initialContent ?? entry.content;
        body.innerHTML = "";
        body.appendChild(textarea);
        actions.innerHTML = "";

        const cancel = document.createElement("button");
        cancel.className = "b3-button b3-button--cancel";
        cancel.textContent = window.siyuan.languages.cancel;
        const submit = document.createElement("button");
        submit.className = "b3-button b3-button--text";
        submit.textContent = window.siyuan.languages.confirm;
        actions.append(cancel, submit);

        const restore = () => {
            this.editingUserEntryID = "";
            if (this.pendingEditDraft?.entryID === entryID) {
                this.pendingEditDraft = null;
            }
            const replacement = this.createUserMessage(entry.content, entry.timestamp, entry.id, entry.blockHTML);
            el.replaceWith(replacement);
            this.renderUserMessage(replacement);
        };
        cancel.addEventListener("click", restore);
        submit.addEventListener("click", async () => {
            const content = textarea.value.trim();
            if (!content) {
                textarea.focus();
                return;
            }
            await this.regenerateResponse(entryID, content);
        });
        textarea.addEventListener("keydown", (event) => {
            if (event.isComposing) {
                return;
            }
            if (event.key === "Escape") {
                event.preventDefault();
                restore();
            } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                event.preventDefault();
                submit.click();
            }
        });
        textarea.focus();
        textarea.setSelectionRange(textarea.value.length, textarea.value.length);
    }

    private createAIMessagePlaceholder(): HTMLElement {
        this.currentContent = "";
        this.currentAssistantEntryId = SessionStore.newSessionId();
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--ai";
        el.setAttribute("data-message-id", this.currentAssistantEntryId);
        el.innerHTML = '<div class="agent-chat__body b3-typography agent-chat__body--streaming"></div>';
        this.messagesContainer.appendChild(el);
        this.scrollToBottom();
        this.observeStickTarget(el);
        return el;
    }

    private pendingTokenUpdate = false;
    private pendingReasoningUpdate = false;
    private rafId = 0;

    private appendToken(token: string) {
        this.currentContent += token;
        this.fullContent += token;

        const thinkBody = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-body");
        if (thinkBody) {
            let chatEl = thinkBody.querySelector(".agent-chat__thinking-chat--streaming") as HTMLElement;
            if (!chatEl) {
                chatEl = document.createElement("div");
                chatEl.className = "agent-chat__thinking-chat b3-typography agent-chat__thinking-chat--streaming";
                thinkBody.appendChild(chatEl);
            }
            if (!this.pendingTokenUpdate) {
                this.pendingTokenUpdate = true;
                // Batch updates with RAF (consistent with regular AI messages), reducing rebuild
                // frequency.
                // Write plain text via textContent while streaming, deferring rich rendering until
                // completion, to avoid re-parsing the whole markdown blob every frame.
                this.rafId = requestAnimationFrame(() => {
                    this.pendingTokenUpdate = false;
                    chatEl.textContent = this.currentContent;
                    const body = chatEl.closest(".agent-chat__thinking-body") as HTMLElement | null;
                    if (body) {
                        body.scrollTop = body.scrollHeight;
                    }
                    this.scrollToBottom();
                });
            }
            return;
        }

        if (!this.currentAIElement) {
            this.currentAIElement = this.createAIMessagePlaceholder();
        }

        if (!this.pendingTokenUpdate) {
            this.pendingTokenUpdate = true;
            // While streaming, only write plain text via textContent, skipping Lute parsing and
            // postRender rich rendering.
            // Rich rendering (highlighting/formulas/charts) is deferred to a single pass in
            // finishResponse, to avoid an O(n^2) rebuild every frame.
            this.rafId = requestAnimationFrame(() => {
                this.pendingTokenUpdate = false;
                const bodyEl = this.currentAIElement?.querySelector(".agent-chat__body") as HTMLElement;
                if (bodyEl) {
                    bodyEl.textContent = this.currentContent;
                    this.scrollToBottom();
                }
            });
        }
    }

    private flushTokenUpdate() {
        if (this.pendingTokenUpdate) {
            this.pendingTokenUpdate = false;
            cancelAnimationFrame(this.rafId);
            // Thinking card streaming: update chatEl and scroll to the bottom (consistent with
            // appendToken's thinking branch).
            // Uses textContent just like appendToken; rich rendering is handled uniformly once
            // finishResponse completes.
            const thinkChat = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-chat--streaming") as HTMLElement;
            if (thinkChat) {
                thinkChat.textContent = this.currentContent;
                const thinkBody = thinkChat.parentElement;
                if (thinkBody) {
                    thinkBody.scrollTop = thinkBody.scrollHeight;
                }
                return;
            }
            const bodyEl = this.currentAIElement?.querySelector(".agent-chat__body") as HTMLElement;
            if (bodyEl) {
                bodyEl.textContent = this.currentContent;
            }
        }
    }

    private appendToolCall(name: string) {
        const body = this.messagesContainer.querySelector(
            ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-body"
        ) as HTMLElement;
        if (!body) {
            return;
        }
        this.toolCallStartedAt.set(name, Date.now());
        if (this.renderedToolNames[name]) {
            this.setToolCallRunning(name, true);
            return;
        }

        this.renderedToolNames[name] = true;
        const lastElement = body.lastElementChild as HTMLElement;
        if (lastElement?.classList.contains("agent-chat__thinking-tools-line")) {
            const toolElement = document.createElement("span");
            toolElement.className = "agent-chat__thinking-tool agent-chat__thinking-tool--running";
            toolElement.textContent = name;
            lastElement.appendChild(toolElement);
        } else {
            body.insertAdjacentHTML("beforeend", renderToolsLineHTML([{name, running: true}]));
        }
        body.scrollTop = body.scrollHeight;
        this.scrollToBottom();
    }

    private setToolCallRunning(name: string, running: boolean) {
        const selector = running
            ? ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-tool"
            : ".agent-chat__thinking-tool--running";
        const toolElements = this.messagesContainer.querySelectorAll(selector);
        for (let i = toolElements.length - 1; i >= 0; i--) {
            const toolElement = toolElements[i];
            if (toolElement.textContent === name) {
                toolElement.classList.toggle("agent-chat__thinking-tool--running", running);
                if (running) {
                    return;
                }
            }
        }
    }

    private finishToolCall(name: string) {
        const stillRunning = this.currentToolCalls.some((item) => item.name === name && item.result === undefined);
        if (stillRunning) {
            return;
        }
        const startedAt = this.toolCallStartedAt.get(name);
        const remaining = startedAt ? Math.max(600 - (Date.now() - startedAt), 0) : 0;
        window.setTimeout(() => {
            if (this.toolCallStartedAt.get(name) !== startedAt ||
                this.currentToolCalls.some((item) => item.name === name && item.result === undefined)) {
                return;
            }
            this.setToolCallRunning(name, false);
            this.toolCallStartedAt.delete(name);
        }, remaining);
    }

    private appendToolResult(name: string, result: string) {
        if (name !== "todo_write") {
            return;
        }

        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--tool";
        el.setAttribute("data-message-id", SessionStore.newSessionId());
        el.innerHTML = renderTodoList(result);
        this.insertBeforeAI(el);
        this.scrollToBottom(true);
        this.hasInterveningCard = true;
    }

    private appendThinking(reasoning: string) {
        const L = window.siyuan.languages;
        if (this.currentThinkingText) {
            // A step does not store text (it is generated from duration via i18n at render time).
            // toolNames only takes the tools newly added this round (those after
            // lastStepToolCount), avoiding accumulating duplicate historical tools — the full
            // arguments/result are stored once in the assistant entry.
            const toolNames = this.currentToolCalls.slice(this.lastStepToolCount).map(function (t) {
                return t.name;
            });
            this.currentThinkingSteps.push({
                reasoning: this.currentThinkingReasoning,
                reasoningContent: this.currentThinkingReasoningContent,
                toolNames: toolNames.length > 0 ? toolNames : undefined,
            });
            this.lastStepToolCount = this.currentToolCalls.length;
        }
        this.currentThinkingText = "";
        this.currentThinkingReasoning = reasoning;
        this.currentThinkingReasoningContent = "";
        const text = L.agentThinking || "Thinking";

        this.currentThinkingText = text;

        let detailLines = "";
        if (reasoning === "processing" && this.currentToolCalls.length > 0) {
            const newTools: Array<{ name: string; running: boolean }> = [];
            for (let i = 0; i < this.currentToolCalls.length; i++) {
                const tc = this.currentToolCalls[i];
                if (!this.renderedToolNames[tc.name]) {
                    this.renderedToolNames[tc.name] = true;
                    const running = tc.result === undefined;
                    if (running) {
                        this.toolCallStartedAt.set(tc.name, Date.now());
                    }
                    newTools.push({name: tc.name, running});
                }
            }
            if (newTools.length > 0) {
                detailLines += renderToolsLineHTML(newTools);
            }
        }

        if (reasoning === "processing" && this.currentAIElement) {
            if (this.currentContent) {
                const bodyEl = this.currentAIElement.querySelector(".agent-chat__body") as HTMLElement;
                if (bodyEl) {
                    bodyEl.classList.remove("agent-chat__body--streaming");
                }
                this.attachStepContent(this.currentContent);
                this.currentAIElement.remove();
            } else {
                this.currentAIElement.remove();
            }
            this.currentAIElement = null;
            this.currentAssistantEntryId = "";
            this.currentContent = "";
        } else if (reasoning === "processing" && this.currentContent) {
            this.attachStepContent(this.currentContent);
            this.currentContent = "";
            const streamingEl = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-chat--streaming") as HTMLElement;
            if (streamingEl) {
                streamingEl.classList.remove("agent-chat__thinking-chat--streaming");
            }
        }

        if (reasoning === "processing" && this.hasInterveningCard) {
            const L = window.siyuan.languages;
            // Aligned with finishActiveThinking: compute this thinking card's duration first, used
            // both for the DOM display "Thought for Xs" and for persisting entry.duration (so the
            // correct duration still shows after a reload).
            const durSec = this.currentThinkingDuration ||
                (this.requestStartTime ? (Date.now() - this.requestStartTime) / 1000 : 0);
            this.currentThinkingDuration = durSec;
            const doneText = durSec > 0
                ? (L.agentThinkingDoneTime ? L.agentThinkingDoneTime.replace("%s", Math.round(durSec) + "s") : (L.agentThinking || "Thinking"))
                : (L.agentThinking || "Thinking");
            const oldCards = this.messagesContainer.querySelectorAll(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)");
            for (let i = 0; i < oldCards.length; i++) {
                const card = oldCards[i] as HTMLElement;
                card.classList.add("agent-chat__msg--thinking-done");
                const txtEl = card.querySelector(".agent-chat__thinking-text");
                if (txtEl) {
                    txtEl.textContent = doneText;
                }
            }
            if (this.currentThinkingStepContent && this.currentThinkingSteps.length > 0) {
                this.currentThinkingSteps[this.currentThinkingSteps.length - 1].content = this.currentThinkingStepContent;
                this.currentThinkingStepContent = "";
            }
            if (this.currentThinkingSteps.length > 0) {
                this.entries.push({
                    id: this.currentThinkingEntryId || undefined,
                    type: "thinking",
                    steps: this.currentThinkingSteps.slice(),
                    duration: this.currentThinkingDuration || undefined
                });
                this.currentThinkingSteps = [];
                this.currentThinkingEntryId = "";
            }
            // Card boundary: one thinking card has been persisted, so reset the tool-name dedup
            // table so the next card independently shows its own round's tools (aligned with the
            // per-card local dedup seenTools used by renderMergedThinkingCard on the reload path).
            this.renderedToolNames = {};
            // Flush tool calls as assistant entry
            if (this.currentToolCalls.length > 0) {
                this.entries.push({
                    id: SessionStore.newSessionId(),
                    type: "assistant",
                    toolCalls: this.slimToolCallsForPersistence(this.currentToolCalls)
                });
                this.currentToolCalls = [];
                this.lastStepToolCount = 0;
            }
            // Flush pending confirms
            if (this.pendingConfirms.length > 0) {
                for (const c of this.pendingConfirms) {
                    this.entries.push(c);
                }
                this.pendingConfirms = [];
            }
            // Interaction cards such as confirm/question interrupt thinking; the new card's timer
            // should restart after the interaction completes.
            this.currentThinkingDuration = 0;
            this.requestStartTime = Date.now();
            this.hasInterveningCard = false;
        }

        const existingCard = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)") as HTMLElement;
        const existingBody = existingCard?.querySelector(".agent-chat__thinking-body");
        if (existingBody) {
            const textEl = existingCard.querySelector(".agent-chat__thinking-text");
            if (textEl) {
                textEl.textContent = text;
            }
            if (detailLines) {
                existingBody.innerHTML += detailLines;
            }
            this.scrollToBottom();
            this.startThinkingTimer();
            return;
        }
        if (existingCard) {
            existingCard.remove();
        }

        const bodyHTML = '<div class="agent-chat__thinking-body agent-chat__thinking-body--preview">' +
            detailLines +
            "</div>";

        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--thinking";
        if (!this.currentThinkingEntryId) {
            this.currentThinkingEntryId = SessionStore.newSessionId();
        }
        el.setAttribute("data-message-id", this.currentThinkingEntryId);
        el.innerHTML = '<div class="agent-chat__thinking-card">' +
            '<div class="agent-chat__thinking-header">' +
            '<span class="agent-chat__thinking-arrow">' +
            '<svg class="agent-chat__thinking-arrow--expand"><use xlink:href="#iconExpand"></use></svg>' +
            '<svg class="agent-chat__thinking-arrow--contract fn__none"><use xlink:href="#iconContract"></use></svg>' +
            "</span>" +
            '<span class="agent-chat__thinking-text">' + escapeHtml(text) + "</span>" +
            "</div>" +
            bodyHTML +
            "</div>";

        bindThinkingCardToggle(el);
        this.insertBeforeAI(el);
        this.scrollToBottom();
        this.observeStickTarget(el);
        this.startThinkingTimer();
    }

    private appendReasoning(token: string) {
        const isNewRound = this.currentThinkingReasoningContent.length === 0;
        this.currentThinkingReasoningContent += token;
        const thinkingElems = this.messagesContainer.querySelectorAll(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-body");
        if (thinkingElems.length === 0) {
            return;
        }
        const thinking = thinkingElems[thinkingElems.length - 1];
        // Create the reasoning element immediately for a new round (to guarantee ordering across
        // rounds); text content is appended via batched RAF updates (reducing reflow).
        if (isNewRound) {
            const reasoningEl = document.createElement("div");
            reasoningEl.className = "agent-chat__thinking-reasoning-text";
            thinking.appendChild(reasoningEl);
        }
        if (!this.pendingReasoningUpdate) {
            this.pendingReasoningUpdate = true;
            requestAnimationFrame(() => {
                this.pendingReasoningUpdate = false;
                const allReasoning = thinking.querySelectorAll(".agent-chat__thinking-reasoning-text");
                const reasoningEl = allReasoning[allReasoning.length - 1] as HTMLElement;
                if (reasoningEl) {
                    reasoningEl.textContent = this.currentThinkingReasoningContent;
                    // The preview state has a fixed height, so scroll to the bottom to keep the
                    // latest reasoning content visible.
                    const body = reasoningEl.closest(".agent-chat__thinking-body") as HTMLElement | null;
                    if (body) {
                        body.scrollTop = body.scrollHeight;
                    }
                }
            });
        }
    }

    private addCopyButton(el: HTMLElement, contentOverride?: string, timestamp?: number) {
        const content = contentOverride || this.fullContent || el.querySelector(".agent-chat__body")?.textContent || "";
        const L = window.siyuan.languages;

        const actions = document.createElement("div");
        actions.className = "agent-chat__msg-actions";

        if (timestamp) {
            const timeSpan = document.createElement("span");
            timeSpan.className = "agent-chat__msg-meta agent-chat__msg-time--ai";
            timeSpan.textContent = this.formatMessageTime(timestamp);
            actions.appendChild(timeSpan);
        }

        const copyBtn = document.createElement("span");
        copyBtn.className = "block__icon block__icon--show ariaLabel";
        copyBtn.setAttribute("data-position", "north");
        copyBtn.setAttribute("aria-label", L.copy);
        copyBtn.innerHTML = '<svg><use xlink:href="#iconCopy"></use></svg>';
        copyBtn.addEventListener("click", (e: Event) => {
            e.stopPropagation();
            navigator.clipboard.writeText(content).then(() => {
                showMessage(window.siyuan.languages.copied, 2000);
            }).catch(() => {
                showMessage(window.siyuan.languages.copied, 2000);
            });
        });
        actions.appendChild(copyBtn);

        const regenBtn = document.createElement("span");
        regenBtn.className = "block__icon block__icon--show ariaLabel";
        regenBtn.setAttribute("data-position", "north");
        regenBtn.setAttribute("aria-label", L.agentRegenerate);
        regenBtn.innerHTML = '<svg><use xlink:href="#iconRefresh"></use></svg>';
        regenBtn.addEventListener("click", (e: Event) => {
            e.stopPropagation();
            this.regenerateResponse(this.findUserEntryIDBeforeElement(el));
        });
        actions.appendChild(regenBtn);

        el.appendChild(actions);
    }

    private findUserEntryIDBeforeElement(el: HTMLElement): string | undefined {
        let current: Element | null = el;
        while (current) {
            if (current.classList.contains("agent-chat__msg--user")) {
                return (current as HTMLElement).dataset.messageId;
            }
            current = current.previousElementSibling;
        }
        return undefined;
    }

    private async confirmHistoryTruncation(entryIndex: number): Promise<boolean> {
        if (!hasAgentExecutedToolsAfter(this.entries, entryIndex)) {
            return true;
        }
        return new Promise((resolve) => {
            confirmDialog(window.siyuan.languages.confirm,
                window.siyuan.languages.agentEditHistoryWarning,
                () => resolve(true), () => resolve(false));
        });
    }

    private async regenerateResponse(userEntryID?: string, editedContent?: string) {
        if (this.isStreaming || this.mirrorLocked || this.modelOptions.length === 0) {
            return;
        }
        if (!await this.prepareForNewTurn()) {
            return;
        }
        const requestSessionID = this.sessionId;
        const requestRevision = SessionStore.getRevision(requestSessionID);
        let targetIndex = findAgentUserEntryIndex(this.entries, userEntryID);
        if (targetIndex < 0) {
            return;
        }
        if (editedContent !== undefined && userEntryID) {
            this.pendingEditDraft = {entryID: userEntryID, content: editedContent};
        }
        if (!await this.confirmHistoryTruncation(targetIndex)) {
            this.restorePendingEditDraft();
            return;
        }
        if (!isAgentRegenerateStateCurrent(requestSessionID, this.sessionId, requestRevision,
            SessionStore.getRevision(requestSessionID), this.isStreaming, this.mirrorLocked)) {
            if (this.sessionId === requestSessionID) {
                this.restorePendingEditDraft();
            } else {
                this.pendingEditDraft = null;
            }
            return;
        }
        targetIndex = findAgentUserEntryIndex(this.entries, userEntryID);
        if (targetIndex < 0) {
            this.restorePendingEditDraft();
            return;
        }
        const targetEntry = this.entries[targetIndex];
        if (targetEntry.type !== "user") {
            return;
        }
        this.editingUserEntryID = "";
        this.pendingEditDraft = editedContent === undefined ? null : {
            entryID: targetEntry.id || "",
            content: editedContent,
        };
        if (editedContent !== undefined) {
            const contentChanged = editedContent !== targetEntry.content;
            targetEntry.content = editedContent;
            if (contentChanged) {
                targetEntry.blockHTML = undefined;
            }
            const references = filterAgentReferencesForContent(targetEntry.references || [], editedContent);
            targetEntry.references = references.length > 0 ? references : undefined;
        }
        this.entries.splice(targetIndex + 1);

        const targetEl = this.messagesContainer.querySelector(
            '.agent-chat__msg--user[data-message-id="' + targetEntry.id + '"]') as HTMLElement | null;
        if (targetEl) {
            let sibling = targetEl.nextElementSibling;
            while (sibling) {
                const next = sibling.nextElementSibling;
                sibling.remove();
                sibling = next;
            }
            if (editedContent !== undefined) {
                const replacement = this.createUserMessage(targetEntry.content, targetEntry.timestamp, targetEntry.id,
                    targetEntry.blockHTML);
                targetEl.replaceWith(replacement);
                this.renderUserMessage(replacement);
            }
        }
        this.currentAIElement = null;
        this.observeStickTarget(null);
        this.currentContent = "";
        this.fullContent = "";
        this.currentToolCalls = [];
        this.lastStepToolCount = 0;
        this.renderedToolNames = {};
        this.hasInterveningCard = false;
        this.currentThinkingSteps = [];
        this.currentThinkingStepContent = "";
        this.currentThinkingText = "";
        this.currentThinkingReasoning = "";
        this.currentThinkingReasoningContent = "";
        this.rebuildNavMarkers();

        // Re-submit
        this.setStreaming(true);
        this.removeMirrorPlaceholder();
        this.requestStartTime = Date.now();
        this.currentThinkingDuration = 0;
        this.currentTurnID = "";
        const lastUserEntry = targetEntry;
        const lastUserText = lastUserEntry.content;
        const editorContext = this.captureEditorContext();
        lastUserEntry.editorContext = editorContext;
        const pluginActions = this.getPluginActions();
        this.abortController = new AbortController();
        const requestSessionId = this.sessionId;
        await fetchAgentSSE(
            lastUserText,
            window.siyuan.config.appearance.lang,
            lastUserEntry.references || [],
            (event: ISSEResult) => {
                if (this.sessionId !== requestSessionId) {
                    return;
                }
                return this.handleSSEEvent(event);
            },
            (err: Error) => {
                if (this.sessionId !== requestSessionId) {
                    return;
                }
                // 409: this session is being chatted in by another instance (instance-level mutual
                // exclusion); do not enter streaming.
                if (err instanceof AgentHttpError && err.status === 409) {
                    return this.handleConflictReject();
                }
                return this.handleConfigError(err, undefined, true);
            },
            this.abortController.signal,
            this.sessionId,
            this.getSelectedModel(),
            this.selectedReasoningEffort,
            true,
            editorContext,
            pluginActions,
            lastUserEntry.id,
            SessionStore.getRevision(this.sessionId),
        );
    }

    // When streaming ends, convert currentAIElement's body from plain text to rich rendering
    // (Lute + postRender) in one pass.
    // Shared by finishResponse (normal completion) and the error path (interruption), ensuring the
    // lightly-rendered content from streaming still ends up fully rich-rendered.
    private finalizeStreamingBody(content: string, ts: number) {
        if (!this.currentAIElement) {
            return;
        }
        const bodyEl = this.currentAIElement.querySelector(".agent-chat__body") as HTMLElement;
        if (!bodyEl) {
            return;
        }
        bodyEl.classList.remove("agent-chat__body--streaming");
        if (content) {
            // Rich rendering runs only once here, avoiding the stutter from an O(n^2) rebuild every
            // frame during streaming.
            bodyEl.innerHTML = this.lute.ProtylePreviewStr("", content) || escapeHtml(content);
            postRender(bodyEl, this.app);
            this.addCopyButton(this.currentAIElement, undefined, ts);
            this.scrollToBottom(true);
        }
    }

    private async finishResponse(notify = true) {
        // Before thinking ends, record the last unfinished thinking card first, used to locate the
        // scroll anchor after it collapses.
        const activeThinkCard = this.messagesContainer.querySelector(
            ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)"
        ) as HTMLElement | null;
        this.finishActiveThinking();
        const savedContent = this.currentContent;
        const savedFullContent = this.fullContent;
        const ts = Date.now();
        // Streaming ended: convert the body from streaming's plain text to a single, complete rich
        // rendering pass (Lute + postRender).
        // Scenario one: the content landed inside a thinking card during streaming (currentAIElement
        // is still empty), so a new regular AI message must be created to hold it.
        if (!this.currentAIElement && savedContent) {
            const thinkBody = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-body");
            if (thinkBody) {
                const streamingEl = thinkBody.querySelector(".agent-chat__thinking-chat--streaming") as HTMLElement;
                if (streamingEl) {
                    streamingEl.remove();
                }
            }
            this.currentAssistantEntryId = SessionStore.newSessionId();
            const el = document.createElement("div");
            el.className = "agent-chat__msg agent-chat__msg--ai";
            el.setAttribute("data-message-id", this.currentAssistantEntryId);
            el.innerHTML = '<div class="agent-chat__body b3-typography">' + (this.lute.ProtylePreviewStr("", savedContent) || escapeHtml(savedContent)) + "</div>";
            this.messagesContainer.appendChild(el);
            postRender(el, this.app);
            this.currentAIElement = el;
            this.currentContent = savedContent;
            this.fullContent = savedFullContent;
            this.addCopyButton(el, undefined, ts);
            // Thinking-ended scenario: scroll to just below the thinking card (card pinned to the
            // top, body expanding downward), rather than straight to the bottom of the conversation.
            if (activeThinkCard) {
                this.scrollToThinkingCardBelow(activeThinkCard);
            } else {
                this.scrollToBottom(true);
            }
        } else if (this.currentAIElement) {
            // Scenario two: a regular streaming element (created by createAIMessagePlaceholder,
            // body still plain text), rich-rendered in one pass.
            this.finalizeStreamingBody(savedContent, ts);
        }
        this.flushThinkingStep();
        if (this.pendingConfirms.length > 0) {
            for (const c of this.pendingConfirms) {
                this.entries.push(c);
            }
            this.pendingConfirms = [];
        }
        if (this.currentContent) {
            this.entries.push({
                id: this.currentAssistantEntryId || undefined,
                type: "assistant",
                content: this.currentContent,
                toolCalls: this.currentToolCalls.length > 0 ? this.slimToolCallsForPersistence(this.currentToolCalls) : undefined,
                timestamp: ts,
            });
        } else if (this.currentToolCalls.length > 0) {
            this.entries.push({
                id: SessionStore.newSessionId(),
                type: "assistant",
                toolCalls: this.slimToolCallsForPersistence(this.currentToolCalls)
            });
        }
        this.currentAIElement = null;
        this.observeStickTarget(null);
        this.currentAssistantEntryId = "";
        this.currentContent = "";
        this.fullContent = "";
        this.currentToolCalls = [];
        this.lastStepToolCount = 0;
        this.renderedToolNames = {};
        if (this.requestStartTime) {
            this.requestStartTime = 0;
        }
        this.updateTokenDisplay();
        const sessionID = this.sessionId;
        const canonicalSession = await this.saveSession(this.currentTurnID);
        this.pendingEditDraft = null;
        if (this.sessionId === sessionID) {
            // On commit, the backend rebuilds this round's assistant/tool results from the runtime.
            // Use the authoritative session from the commit response directly, to avoid the next
            // regular save overwriting it with the frontend's streaming snapshot again, and to avoid
            // the failure/ordering window of an extra GET.
            if (canonicalSession) {
                const atBottom = this.isScrolledToBottom();
                const savedScroll = this.messagesContainer.scrollTop;
                this.entries = this.buildEntriesFromSession(canonicalSession);
                this.updateMetaFromSession(canonicalSession);
                this.messagesContainer.innerHTML = "";
                this.renderLoadedSession(canonicalSession);
                if (atBottom) {
                    this.scrollToBottom(true);
                } else {
                    this.messagesContainer.scrollTop = savedScroll;
                }
            } else {
                await this.reloadFromDisk(true);
            }
        }
        this.setStreaming(false);
        if (this.pendingSessionTitle !== null && this.sessionId === sessionID) {
            await this.saveSession();
        }
        this.rebuildNavMarkers();
        if (notify && savedContent && (!document.hasFocus() || document.hidden)) {
            const L = window.siyuan.languages;
            sendNotification({title: L.agentNotifyDone, timeoutType: "default"});
        }
    }

    // Attribute the content that was displayed inside the thinking card in the previous round to
    // the last step just pushed, and clear currentThinkingStepContent. This way each step's content
    // is correctly attributed to its own round, instead of being mistakenly attached by
    // flushThinkingStep to the next round's step (which would misplace content after a reload).
    private attachStepContent(content: string) {
        if (content && this.currentThinkingSteps.length > 0) {
            this.currentThinkingSteps[this.currentThinkingSteps.length - 1].content = content;
        }
        this.currentThinkingStepContent = "";
    }

    private flushThinkingStep() {
        if (this.currentThinkingText) {
            const toolNames = this.currentToolCalls.slice(this.lastStepToolCount).map(function (t) {
                return t.name;
            });
            this.currentThinkingSteps.push({
                reasoning: this.currentThinkingReasoning,
                reasoningContent: this.currentThinkingReasoningContent,
                toolNames: toolNames.length > 0 ? toolNames : undefined,
                content: this.currentThinkingStepContent || undefined,
            });
            this.lastStepToolCount = this.currentToolCalls.length;
            this.currentThinkingText = "";
            this.currentThinkingStepContent = "";
        }
        if (this.currentThinkingSteps.length > 0) {
            this.entries.push({
                id: this.currentThinkingEntryId || undefined,
                type: "thinking",
                steps: this.currentThinkingSteps.slice(),
                duration: this.currentThinkingDuration || undefined,
            });
            this.currentThinkingSteps = [];
            this.currentThinkingEntryId = "";
            // Card boundary: consistent with appendThinking's hasInterveningCard branch, reset the
            // tool-name dedup table.
            this.renderedToolNames = {};
        }
    }

    private tryGenerateTitle() {
        if (this.hasTitled) {
            return;
        }
        this.hasTitled = true;
        const requestSessionID = this.sessionId;
        const userEntry = this.entries.find((e): e is { type: "user"; content: string } => e.type === "user");
        const userMsg = userEntry?.content?.slice(0, 500) || "";
        fetch("/api/ai/agent/title", {
            method: "POST",
            headers: {"Content-Type": "application/json"},
            body: JSON.stringify({
                message: userMsg,
                model: this.getSelectedModel(),
                language: window.siyuan.config.appearance.lang
            }),
        }).then((resp) => resp.json()).then((data) => {
            if (this.sessionId === requestSessionID && data.code === 0 && data.data && data.data !== this.sessionTitle) {
                this.sessionTitle = data.data;
                this.pendingSessionTitle = data.data;
                this.titleElement.textContent = data.data;
                // The unified commit when streaming ends will include the latest title; saving it
                // separately mid-stream would change the content revision number, causing the
                // not-yet-created runtime turn to be mistaken for a stale request.
                if (!this.isStreaming && !this.currentTurnID) {
                    void this.saveSession();
                }
            }
        }).catch((e) => {
            console.error("agent title request error:", e);
        });
    }

    private appendError(message: string) {
        this.finishActiveThinking();
        this.clearThinking();
        if (this.currentAIElement && !this.currentContent) {
            this.currentAIElement.remove();
        }
        this.currentAIElement = null;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--error";
        el.innerHTML = '<div class="agent-chat__body agent-chat__body--error"><svg class="agent-chat__error-icon"><use xlink:href="#iconTriangleAlert"></use></svg><span>' + escapeHtml(message) + "</span></div>";
        this.messagesContainer.appendChild(el);
        this.scrollToBottom(true);
        this.flushThinkingStep();
    }

    private appendRetry(attempt: number, maxRetries: number) {
        this.finishActiveThinking();
        this.currentThinkingSteps = [];
        this.currentThinkingStepContent = "";
        this.renderedToolNames = {};
        this.clearThinking();
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--retry";
        el.innerHTML = renderRetryCardHTML(attempt, maxRetries);
        this.insertBeforeAI(el);
        this.scrollToBottom(true);
        this.hasInterveningCard = true;
    }

    private appendSnapshotInfo(snapshotID: string, entryId?: string) {
        const L = window.siyuan.languages;
        const shortID = snapshotID.length > 7 ? snapshotID.substring(0, 7) : snapshotID;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--snapshot";
        if (entryId) {
            el.setAttribute("data-message-id", entryId);
        }
        el.innerHTML = '<div class="agent-chat__snapshot-body">' +
            '<span class="agent-chat__snapshot-icon"><svg><use xlink:href="#iconHistory"></use></svg></span>' +
            '<span class="agent-chat__snapshot-text">' + escapeHtml((L.snapshotAutoCreated || "Auto snapshot created") + " " + shortID) + "</span>" +
            '<button class="b3-button b3-button--text agent-chat__snapshot-rollback ariaLabel" aria-label="' + (L.rollback || "Rollback") + '"><svg><use xlink:href="#iconUndo"></use></svg></button>' +
            "</div>";
        const rollbackBtn = el.querySelector(".agent-chat__snapshot-rollback") as HTMLButtonElement;
        rollbackBtn.addEventListener("click", () => {
            const confirmText = (L.rollbackConfirm || "Rollback cannot be undone").replace("${name}", L.dataSnapshot || "Snapshot").replace("${time}", shortID);
            confirmDialog("⚠️ " + (L.rollback || "Rollback"), confirmText, () => {
                fetchPost("/api/repo/checkoutRepo", {id: snapshotID, sessionID: this.sessionId}, () => {
                    // Record the rollback operation, so the "rolled back" hint remains visible after
                    // reloading the session (activates the appendRollbackInfo render branch).
                    const rollbackEntryId = SessionStore.newSessionId();
                    this.entries.push({id: rollbackEntryId, type: "rollback", snapshotID: snapshotID});
                    this.appendRollbackInfo(snapshotID, rollbackEntryId);
                    void this.saveSession();
                });
            });
        });
        // The snapshot should come before the execution area: insert before the confirm card if one
        // exists, otherwise look for an active thinking card
        const confirmCards = this.messagesContainer.querySelectorAll(".agent-chat__msg--confirm");
        if (confirmCards.length > 0) {
            this.messagesContainer.insertBefore(el, confirmCards[confirmCards.length - 1]);
        } else {
            const activeThinking = this.messagesContainer.querySelector(
                ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)"
            );
            if (activeThinking) {
                this.messagesContainer.insertBefore(el, activeThinking);
            } else {
                this.insertBeforeAI(el);
            }
        }
        this.scrollToBottom(true);
        this.hasInterveningCard = true;
    }

    private appendRollbackInfo(snapshotID: string, entryId?: string) {
        const L = window.siyuan.languages;
        const shortID = snapshotID.length > 7 ? snapshotID.substring(0, 7) : snapshotID;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--snapshot";
        if (entryId) {
            el.setAttribute("data-message-id", entryId);
        }
        el.innerHTML = '<div class="agent-chat__snapshot-body">' +
            '<span class="agent-chat__snapshot-icon"><svg><use xlink:href="#iconHistory"></use></svg></span>' +
            '<span class="agent-chat__snapshot-text">' + escapeHtml((L.rollbackCompleted || "Rollback completed") + " " + shortID) + "</span>" +
            "</div>";
        this.messagesContainer.appendChild(el);
        this.scrollToBottom(true);
    }

    private async stopGeneration() {
        if (this.abortController) {
            this.abortController.abort();
            this.abortController = null;
        }
        this.flushTokenUpdate();
        this.finishActiveThinking();
        const savedContent = this.currentContent;
        const savedFullContent = this.fullContent;
        const ts = Date.now();
        if (!this.currentAIElement && savedContent) {
            const thinkBody = this.messagesContainer.querySelector(".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-body");
            if (thinkBody) {
                const streamingEl = thinkBody.querySelector(".agent-chat__thinking-chat--streaming") as HTMLElement;
                if (streamingEl) {
                    streamingEl.remove();
                }
            }
            this.currentAssistantEntryId = SessionStore.newSessionId();
            const el = document.createElement("div");
            el.className = "agent-chat__msg agent-chat__msg--ai";
            el.setAttribute("data-message-id", this.currentAssistantEntryId);
            el.innerHTML = '<div class="agent-chat__body b3-typography">' + (this.lute.ProtylePreviewStr("", savedContent) || escapeHtml(savedContent)) + "</div>";
            this.messagesContainer.appendChild(el);
            postRender(el, this.app);
            this.currentAIElement = el;
            this.currentContent = savedContent;
            this.fullContent = savedFullContent;
            this.addCopyButton(el, undefined, ts);
            this.scrollToBottom(true);
        }
        this.flushThinkingStep();
        if (this.currentContent) {
            this.entries.push({
                id: this.currentAssistantEntryId || undefined,
                type: "assistant",
                content: this.currentContent,
                toolCalls: this.currentToolCalls.length > 0 ? this.slimToolCallsForPersistence(this.currentToolCalls) : undefined,
                timestamp: ts,
            });
        }
        this.currentAIElement = null;
        this.observeStickTarget(null);
        this.currentAssistantEntryId = "";
        this.currentContent = "";
        this.fullContent = "";
        this.currentToolCalls = [];
        this.lastStepToolCount = 0;
        this.renderedToolNames = {};
        if (this.requestStartTime) {
            this.requestStartTime = 0;
        }
        this.updateTokenDisplay();
        this.setStreaming(false);
        const sessionID = this.sessionId;
        const turnID = this.currentTurnID;
        // abort only interrupts the frontend connection; an external tool may still be in flight.
        // We must not save and clear the runtime directly here — wait until the backend has written
        // the interrupted checkpoint, then recover and commit. The turn event may not have arrived
        // yet, so we poll even with an empty ID.
        try {
            await this.reloadFromDisk(true);
        } catch (e) {
            console.error("reload agent session after stop failed:", e);
        }
        if (this.sessionId === sessionID) {
            void this.recoverInterruptedTurn(sessionID, turnID);
        }
        this.rebuildNavMarkers();
    }

    private insertBeforeAI(el: HTMLElement) {
        if (this.currentAIElement) {
            this.messagesContainer.insertBefore(el, this.currentAIElement);
        } else {
            this.messagesContainer.appendChild(el);
        }
    }

    private renderConfirmEffects(effects?: IToolEffects) {
        if (!effects) {
            return "";
        }
        const L = window.siyuan.languages;
        const items: string[] = [];
        if (effects.dataEgress) {
            items.push(L.agentEffectDataEgress);
        }
        if (effects.externalCost) {
            items.push(L.agentEffectExternalCost);
        }
        if (effects.localWrite) {
            items.push(L.agentEffectLocalWrite);
        }
        if (items.length === 0) {
            return "";
        }
        return '<ul class="agent-chat__confirm-effects">' + items.map((item) => `<li>${escapeHtml(item)}</li>`).join("") + "</ul>";
    }

    private async appendConfirm(name: string, args: Record<string, unknown>, confirmID: string, effects?: IToolEffects) {
        this.finishActiveThinking();
        this.flushThinkingStep();
        const L = window.siyuan.languages;
        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--confirm";
        const argsStr = JSON.stringify(args, null, 2);
        const desc = (L.agentConfirmDesc || "Agent: {category} operation").replace("{category}", escapeHtml(this.toolCategory(name)));
        el.innerHTML = '<div class="agent-chat__confirm-card">' +
            '<div class="agent-chat__confirm-header"><svg class="agent-chat__confirm-icon"><use xlink:href="#iconInfo"></use></svg> ' + desc + "</div>" +
            this.renderConfirmEffects(effects) +
            '<pre class="agent-chat__confirm-args">' + escapeHtml(argsStr) + "</pre>" +
            '<div class="agent-chat__confirm-actions">' +
            '<button class="b3-button b3-button--cancel agent-chat__confirm-reject">' + (L.agentConfirmReject || "Reject") + "</button>" +
            '<button class="b3-button b3-button--text agent-chat__confirm-approve">' + (L.agentConfirmApprove || "Approve") + "</button>" +
            '<button class="b3-button b3-button--text agent-chat__confirm-always ariaLabel" data-position="n" aria-label="' + (L.agentConfirmAlwaysDesc || "Session Allow") + '">' + (L.agentConfirmAlways || "Session Allow") + "</button>" +
            "</div>" +
            "</div>";
        const sessionID = this.sessionId;
        const confirmEntryId = SessionStore.newSessionId();
        const confirmEntry: SessionEntry = {
            id: confirmEntryId,
            type: "confirm",
            name,
            args,
            confirmID,
            effects,
            status: "pending",
        };
        el.setAttribute("data-message-id", confirmEntryId);
        this.pendingConfirms.push(confirmEntry);
        const submitConfirm = async (approved: boolean, always: boolean, doneText: string) => {
            const buttons = Array.from(el.querySelectorAll("button")) as HTMLButtonElement[];
            buttons.forEach((button) => button.disabled = true);
            const accepted = await this.postConfirm(confirmID, approved, always, sessionID, confirmEntryId);
            if (!accepted) {
                buttons.forEach((button) => button.disabled = false);
                showMessage(window.siyuan.languages._kernel[28], 3000);
                return;
            }
            el.classList.add("agent-chat__msg--confirmed");
            const actions = el.querySelector(".agent-chat__confirm-actions") as HTMLElement;
            if (actions) {
                actions.innerHTML = '<span class="agent-chat__confirm-done">' + doneText + "</span>";
            }
        };
        const approveBtn = el.querySelector(".agent-chat__confirm-approve");
        if (approveBtn) {
            approveBtn.addEventListener("click", (e) => {
                e.stopPropagation();
                void submitConfirm(true, false, L.agentConfirmApprove || "Approved");
            });
        }
        const rejectBtn = el.querySelector(".agent-chat__confirm-reject");
        if (rejectBtn) {
            rejectBtn.addEventListener("click", (e) => {
                e.stopPropagation();
                void submitConfirm(false, false, L.agentConfirmReject || "Rejected");
            });
        }
        const alwaysBtn = el.querySelector(".agent-chat__confirm-always");
        if (alwaysBtn) {
            alwaysBtn.addEventListener("click", (e) => {
                e.stopPropagation();
                void submitConfirm(true, true, L.agentConfirmAlways || "Session Allow");
            });
        }
        this.insertBeforeAI(el);
        this.scrollToBottom(true);
        this.hasInterveningCard = true;
        if (!document.hasFocus() || document.hidden) {
            sendNotification({title: L.agentNotifyConfirm, body: "", timeoutType: "default"});
        }
    }

    private async postConfirm(confirmID: string, approved: boolean, always: boolean,
                              sessionID: string, confirmEntryID: string): Promise<boolean> {
        const body: Record<string, unknown> = {confirmID: confirmID, approved: approved};
        if (always) {
            body.always = true;
        }
        try {
            const resp = await fetch("/api/ai/agent/confirm", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(body),
            });
            const result = await resp.json() as {code?: number};
            if (!resp.ok || result?.code !== 0) {
                console.error("agent confirm request failed:", resp.status);
                return false;
            }
        } catch (e) {
            console.error("agent confirm request error:", e);
            return false;
        }
        if (this.sessionId !== sessionID) {
            return true;
        }
        const entry = (this.entries.find(e => e.id === confirmEntryID) ||
            this.pendingConfirms.find(e => e.id === confirmEntryID)) as {status?: string} | undefined;
        if (entry) {
            entry.status = always ? "always" : (approved ? "approved" : "rejected");
        }
        try {
            await this.saveSession();
        } catch (e) {
            console.error("save agent confirmation state failed:", e);
        }
        return true;
    }

    private async handleFrontendToolCall(callID: string, args: Record<string, unknown>) {
        // Resolve the action name ("frontend" tool calls carry the action in args.action).
        const action = (args.action as string | undefined) || "";
        const handler = lookupAction(action);
        if (!handler) {
            await this.postFrontendResult(callID, `Unknown frontend action: ${action}`, true);
            return;
        }
        try {
            const outcome = await handler.handler(args, this.app);
            const result = outcome.result || "";
            const error = outcome.error || "";
            await this.postFrontendResult(callID, error ? error : result, !!error);
        } catch (e) {
            await this.postFrontendResult(callID, `Frontend action threw: ${(e as Error).message}`, true);
        }
    }

    private async postFrontendResult(callID: string, result: string, isError: boolean) {
        for (let attempt = 0; attempt < 3; attempt++) {
            try {
                const resp = await fetch("/api/ai/agent/frontendToolResult", {
                    method: "POST",
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({callID, result, isError}),
                });
                const response = await resp.json() as {code?: number};
                if (resp.ok && response?.code === 0) {
                    return;
                }
                if (resp.status === 409) {
                    console.error("agent frontend result expired:", callID);
                    return;
                }
            } catch (e) {
                if (attempt === 2) {
                    console.error("agent frontend result request error:", e);
                }
            }
            await new Promise((resolve) => window.setTimeout(resolve, 200 * (attempt + 1)));
        }
    }

    private appendQuestion(questionID: string, args: Record<string, unknown>) {
        this.finishActiveThinking();
        this.flushThinkingStep();
        const L = window.siyuan.languages;
        const rawQuestions = args.questions as Array<Record<string, unknown>>;
        if (!rawQuestions || rawQuestions.length === 0) {
            return;
        }

        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--question";
        el.setAttribute("data-question-id", questionID);

        el.innerHTML = renderQuestionCardHTML(rawQuestions, questionID);
        const sessionID = this.sessionId;
        const questionEntryId = SessionStore.newSessionId();
        el.setAttribute("data-message-id", questionEntryId);
        this.entries.push({
            id: questionEntryId,
            type: "question",
            questionID: questionID,
            questions: rawQuestions,
            status: "pending",
        });

        el.querySelectorAll(".agent-chat__question-option").forEach((option) => {
            const input = option.querySelector("input") as HTMLInputElement;
            if (!input) return;
            let wasChecked = false;
            option.addEventListener("mousedown", () => {
                wasChecked = input.checked;
            });
            option.addEventListener("click", (e) => {
                if (el.classList.contains("agent-chat__msg--confirmed")) {
                    return;
                }
                if (input.type === "radio" && wasChecked) {
                    e.preventDefault();
                    input.checked = false;
                }
            });
        });

        const submitBtn = el.querySelector(".agent-chat__question-submit-btn");
        if (submitBtn) {
            submitBtn.addEventListener("click", async () => {
                const answers: string[] = [];
                for (let qi = 0; qi < rawQuestions.length; qi++) {
                    const optEl = el.querySelector('.agent-chat__question-options[data-qi="' + qi + '"]');
                    if (optEl) {
                        const selected = optEl.querySelectorAll("input:checked") as NodeListOf<HTMLInputElement>;
                        for (let si = 0; si < selected.length; si++) {
                            answers.push(selected[si].value);
                        }
                    }
                    const customInput = el.querySelector('.agent-chat__question-custom[data-qi="' + qi + '"]') as HTMLInputElement;
                    if (customInput && customInput.value.trim()) {
                        answers.push(customInput.value.trim());
                    }
                }
                const inputs = Array.from(el.querySelectorAll("input")) as HTMLInputElement[];
                (submitBtn as HTMLButtonElement).disabled = true;
                inputs.forEach((input) => input.disabled = true);
                const accepted = await this.postQuestionAnswer(questionID, answers, sessionID, questionEntryId);
                if (!accepted) {
                    (submitBtn as HTMLButtonElement).disabled = false;
                    inputs.forEach((input) => input.disabled = false);
                    showMessage(window.siyuan.languages._kernel[28], 3000);
                    return;
                }
                el.classList.add("agent-chat__msg--confirmed");
                const actions = el.querySelector(".agent-chat__question-submit");
                if (actions) {
                    (actions as HTMLElement).innerHTML = '<span class="agent-chat__confirm-done">' + (L.agentQuestionSubmitted || "Submitted") + "</span>";
                }
            });
        }

        this.insertBeforeAI(el);
        this.scrollToBottom(true);
        this.hasInterveningCard = true;
    }

    private async postQuestionAnswer(questionID: string, answers: string[],
                                     sessionID: string, questionEntryID: string): Promise<boolean> {
        try {
            const resp = await fetch("/api/ai/agent/question", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({questionID: questionID, answers: answers}),
            });
            const result = await resp.json() as {code?: number};
            if (!resp.ok || result?.code !== 0) {
                console.error("agent question request failed:", resp.status);
                return false;
            }
        } catch (e) {
            console.error("agent question request error:", e);
            return false;
        }
        if (this.sessionId !== sessionID) {
            return true;
        }
        const entry = this.entries.find(e => e.id === questionEntryID) as {
            status?: string; answers?: string[]
        } | undefined;
        if (entry) {
            entry.status = "submitted";
            entry.answers = answers;
        }
        try {
            await this.saveSession();
        } catch (e) {
            console.error("save agent question state failed:", e);
        }
        return true;
    }

    private renderSingleThinkingCard(step: {
        reasoning: string;
        text: string;
        toolNames?: string[];
        reasoningContent: string
    }) {
        const el = createThinkingCardElement(step);
        bindThinkingCardToggle(el);
        this.messagesContainer.appendChild(el);
    }

    private renderMergedThinkingCard(steps: Array<{
        reasoning: string;
        reasoningContent: string;
        toolNames?: string[];
        content?: string
    }>, entryId?: string, duration?: number) {
        if (!steps || steps.length === 0) {
            return;
        }
        let detail = "";
        const seenTools: Record<string, boolean> = {};
        for (let i = 0; i < steps.length; i++) {
            const step = steps[i];
            if (step.content) {
                detail += '<div class="agent-chat__thinking-chat b3-typography">' + (this.lute.ProtylePreviewStr("", step.content) || escapeHtml(step.content)) + "</div>";
            }
            if (step.reasoningContent) {
                detail += '<div class="agent-chat__thinking-reasoning-text">' + escapeHtml(step.reasoningContent) + "</div>";
            }
            // Place the tool-calls line after this step's reasoning, matching how live streaming
            // renders it in practice: "reasoning/content arrive first, and the tool-calls line is
            // appended at the end when the next thinking round comes in".
            const names = step.toolNames && step.toolNames.length > 0
                ? step.toolNames
                : undefined;
            if (names && names.length > 0) {
                const newTools = names.filter(n => !seenTools[n]);
                if (newTools.length > 0) {
                    detail += "<div class=\"agent-chat__thinking-tools-line\"><span class=\"agent-chat__thinking-summary\">Tool calls:</span>";
                    for (let j = 0; j < newTools.length; j++) {
                        seenTools[newTools[j]] = true;
                        detail += '<span class="agent-chat__thinking-tool">' + escapeHtml(newTools[j]) + "</span>";
                    }
                    detail += "</div>";
                }
            }
        }

        const headerText = this.formatThinkingHeader(duration);

        const el = document.createElement("div");
        el.className = "agent-chat__msg agent-chat__msg--thinking agent-chat__msg--thinking-done";
        if (entryId) {
            el.setAttribute("data-message-id", entryId);
        }
        el.innerHTML = '<div class="agent-chat__thinking-card">' +
            '<div class="agent-chat__thinking-header">' +
            '<span class="agent-chat__thinking-arrow">' +
            '<svg class="agent-chat__thinking-arrow--expand"><use xlink:href="#iconExpand"></use></svg>' +
            '<svg class="agent-chat__thinking-arrow--contract fn__none"><use xlink:href="#iconContract"></use></svg>' +
            "</span>" +
            '<span class="agent-chat__thinking-text">' + escapeHtml(headerText) + "</span>" +
            "</div>" +
            '<div class="agent-chat__thinking-body">' +
            detail +
            "</div>" +
            "</div>";

        bindThinkingCardToggle(el);
        this.messagesContainer.appendChild(el);
        postRender(el, this.app);
    }

    // Generate the "Thought for Xs" title text from duration via i18n; fall back to "Thinking..."
    // when there is no duration.
    private formatThinkingHeader(duration?: number): string {
        const L = window.siyuan.languages;
        if (duration && duration > 0) {
            return L.agentThinkingDoneTime ? L.agentThinkingDoneTime.replace("%s", Math.round(duration) + "s") : (L.agentThinking || "Thinking");
        }
        return L.agentThinking || "Thinking";
    }

    // Refresh the token ring display at the bottom. Hide it when contextTokens is 0 (including when
    // switching to an old session with no stats).
    private updateTokenDisplay() {
        if (!this.tokenDisplayEl) {
            return;
        }
        if (this.contextTokens === 0) {
            this.tokenDisplayEl.classList.add("fn__none");
            return;
        }
        this.tokenDisplayEl.classList.remove("fn__none");
        const arc = this.tokenDisplayEl.querySelector(".agent-chat__tokens-arc") as SVGCircleElement | null;
        if (!arc) {
            return;
        }
        const circumference = 2 * Math.PI * 9; // r=9 → ≈56.55
        const tokens = this.contextTokens;
        const limit = this.contextLimit;
        // Arc length: with a known limit, use the actual usage ratio; with an unknown limit
        // (limit=0), draw no arc (only the gray track ring remains).
        // The color is always the primary color; it is no longer tiered by usage ratio.
        const ratio = limit > 0 ? Math.min(tokens / limit, 1) : 0;
        const filled = circumference * ratio;
        arc.setAttribute("stroke-dasharray", filled.toFixed(2) + " " + circumference.toFixed(2));
    }

    // Record the most recent round's prompt tokens (= current context usage) plus the category
    // breakdown, cache hits, and model limit; this is an overwrite update, not accumulation.
    private appendUsage(lastPromptTokens: number, tokenBreakdown: Record<string, number>, cachedTokens: number, contextLimit: number) {
        this.contextTokens = lastPromptTokens;
        this.contextTokenBreakdown = tokenBreakdown;
        this.contextCachedTokens = cachedTokens;
        this.contextLimit = contextLimit;
        this.updateTokenDisplay();
    }

    // Pop up the token breakdown panel. Do not pop it up when the breakdown is all zero (nothing to
    // show).
    private showTokenBreakdownPopup() {
        if (!this.formatTokenBreakdown().length && this.contextCachedTokens === 0) {
            return;
        }
        this.closeTokenBreakdownPopup();
        const L = window.siyuan.languages;
        const popup = document.createElement("div");
        popup.className = "agent-token-popup b3-menu";
        let html = '<div class="b3-menu__items">';
        // First row: used / limit - usage percentage; only show used when the limit is unknown.
        const limitLine = this.contextLimit > 0
            ? this.formatTokenCount(this.contextTokens) + " / " + this.formatTokenCount(this.contextLimit) + " · " + Math.round(this.contextTokens / this.contextLimit * 100) + "%"
            : this.formatTokenCount(this.contextTokens);
        html += '<div class="agent-token-popup__total">' +
            '<span class="agent-token-popup__label">' + (L.tokenUsage || "Context Usage") + "</span>" +
            '<span class="agent-token-popup__value">' + limitLine + "</span>" +
            "</div>";
        // The usage bar below the first row: total length = limit, fill = used ratio, color always
        // the primary color.
        // With an unknown limit (contextLimit=0), draw no bar (there is no total-length baseline).
        if (this.contextLimit > 0) {
            const ratio = Math.min(this.contextTokens / this.contextLimit, 1);
            html += '<div class="agent-token-popup__bar">' +
                '<span style="width:' + (ratio * 100).toFixed(1) + '%"></span>' +
                "</div>";
        } else {
            html += '<div class="agent-token-popup__divider"></div>';
        }
        // Each category (zero values skipped), formatted as a percentage.
        for (const row of this.formatTokenBreakdown()) {
            html += '<div class="agent-token-popup__row">' +
                '<span class="agent-token-popup__label">' + escapeHtml(row.label) + "</span>" +
                '<span class="agent-token-popup__value">' + row.percent + "</span>" +
                "</div>";
        }
        // Cache hits (an independent dimension, separated by a divider; hidden when 0 — for models
        // that do not return cache fields, the whole row is omitted).
        if (this.contextCachedTokens > 0 && this.contextTokens > 0) {
            html += '<div class="agent-token-popup__divider"></div>';
            const cachedPercent = Math.round(this.contextCachedTokens / this.contextTokens * 1000) / 10;
            html += '<div class="agent-token-popup__row">' +
                '<span class="agent-token-popup__label">' + (L.tokenCatCached || "Cache Hits") + "</span>" +
                '<span class="agent-token-popup__value">' + cachedPercent + "%</span>" +
                "</div>";
        }
        html += "</div>";
        popup.innerHTML = html;
        document.body.appendChild(popup);
        popup.style.zIndex = (++window.siyuan.zIndex).toString();
        // Positioning: consistent with the model-select popup — right-aligned to the trigger's
        // right edge (fixed width 280px), vertically below the trigger.
        const rect = this.tokenDisplayEl.getBoundingClientRect();
        setPosition(popup, rect.right - 280, rect.bottom, rect.height, rect.width);
        // Hovering the popup itself keeps it shown (cancel the close timer on mouse enter, close on
        // mouse leave).
        popup.addEventListener("mouseenter", () => {
            window.clearTimeout(this.tokenPopupHideTimer);
        });
        popup.addEventListener("mouseleave", () => {
            this.tokenPopupHideTimer = window.setTimeout(() => {
                this.closeTokenBreakdownPopup();
            }, 300);
        });
        // Closed by clicking outside / resize / ESC.
        popup.addEventListener("click", (e: MouseEvent) => {
            e.stopPropagation();
        });
        // Closed by clicking outside / resize. Listeners are stored as fields so
        // closeTokenBreakdownPopup can clean them up uniformly, avoiding leaks.
        this.tokenPopupOutsideClickHandler = () => {
            this.closeTokenBreakdownPopup();
        };
        this.tokenPopupResizeHandler = () => {
            this.closeTokenBreakdownPopup();
        };
        setTimeout(() => {
            if (this.tokenPopupOutsideClickHandler) {
                document.addEventListener("click", this.tokenPopupOutsideClickHandler);
            }
        }, 10);
        window.addEventListener("resize", this.tokenPopupResizeHandler);
        this.tokenPopup = popup;
    }

    private closeTokenBreakdownPopup() {
        // Uniformly clean up outside listeners, to avoid listeners accumulating across repeated
        // popup opens/closes and causing a memory leak.
        if (this.tokenPopupOutsideClickHandler) {
            document.removeEventListener("click", this.tokenPopupOutsideClickHandler);
            this.tokenPopupOutsideClickHandler = null;
        }
        if (this.tokenPopupResizeHandler) {
            window.removeEventListener("resize", this.tokenPopupResizeHandler);
            this.tokenPopupResizeHandler = null;
        }
        if (this.tokenPopup) {
            this.tokenPopup.remove();
            this.tokenPopup = null;
        }
    }

    // Format contextTokenBreakdown (the backend's estimated 9 categories + other) into
    // [{label, percent}], skipping zero values.
    // percent = category tokens / contextTokens * 100 (shows "-" when contextTokens is 0).
    private formatTokenBreakdown(): Array<{ label: string; percent: string }> {
        const L = window.siyuan.languages;
        // Displayed in a fixed order (matching the backend keys).
        const order: Array<{ key: string; labelKey: string }> = [
            {key: "system", labelKey: "tokenCatSystem"},
            {key: "skills", labelKey: "tokenCatSkills"},
            {key: "messages", labelKey: "tokenCatMessages"},
            {key: "nativeToolsDef", labelKey: "tokenCatNativeToolsDef"},
            {key: "pluginToolsDef", labelKey: "tokenCatPluginToolsDef"},
            {key: "mcpToolsDef", labelKey: "tokenCatMcpToolsDef"},
            {key: "nativeTool", labelKey: "tokenCatNativeTool"},
            {key: "pluginTool", labelKey: "tokenCatPluginTool"},
            {key: "mcpTool", labelKey: "tokenCatMcpTool"},
            {key: "other", labelKey: "tokenCatOther"},
        ];
        const result: Array<{ label: string; percent: string }> = [];
        for (const item of order) {
            const tokens = this.contextTokenBreakdown[item.key] || 0;
            if (tokens <= 0) {
                continue;
            }
            // Keep the percentage to 1 decimal place; skip categories that round to 0 (negligible
            // share) to avoid showing a meaningless 0%.
            const rounded = this.contextTokens > 0
                ? Math.round(tokens / this.contextTokens * 1000) / 10
                : 0;
            if (rounded <= 0) {
                continue;
            }
            const label = (L as Record<string, string>)[item.labelKey] || item.key;
            result.push({label, percent: rounded + "%"});
        }
        return result;
    }

    // Token count formatting: convert base-1024 values (2^N, e.g. 131072 = 128*1024) into the
    // industry-conventional names (128k, 1M).
    // Only use base 1024 when the value "divides evenly by 1024 and the quotient is on the
    // whitelist", to avoid a base-1000 value like 256000 being misread as 250k.
    // Everything else uses base 1000 (200000 -> 200k, 1048576 -> 1.0M).
    private formatTokenCount(n: number): string {
        if (n <= 0) {
            return String(n);
        }
        // Whitelist: common 2^N quotients in the industry (8k/16k/32k/64k/128k/256k/512k/1M) + 200
        // (200k = 204800, uncommon but exists).
        const niceMultiples = new Set([8, 16, 32, 64, 128, 200, 256, 512, 1024]);
        if (n >= 1024 && n % 1024 === 0 && niceMultiples.has(n / 1024)) {
            const quotient = n / 1024;
            if (quotient >= 1024) {
                return (quotient / 1024) + "M";
            }
            return quotient + "k";
        }
        // Base 1000 or otherwise: divide by 1000 / 1000000, omitting the decimal when it divides
        // evenly (200000 -> 200k, 3500 -> 3.5k).
        if (n >= 1000000) {
            return (n / 1000000).toFixed(n % 1000000 === 0 ? 0 : 1) + "M";
        }
        if (n >= 1000) {
            return (n / 1000).toFixed(n % 1000 === 0 ? 0 : 1) + "k";
        }
        return String(n);
    }

    private clearThinking() {
        const items = this.messagesContainer.querySelectorAll(
            ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)"
        );
        for (let i = 0; i < items.length; i++) {
            items[i].remove();
        }
    }

    // Start the thinking timer, refreshing the title text of all unfinished thinking cards to
    // "Thinking... X.Xs" every 100ms.
    private startThinkingTimer() {
        this.stopThinkingTimer();
        if (!this.requestStartTime) {
            return;
        }
        const tick = () => {
            const sec = Math.floor((Date.now() - this.requestStartTime) / 1000);
            const L = window.siyuan.languages;
            const live = (L.agentThinking || "Thinking") + " " + sec + "s";
            const cards = this.messagesContainer.querySelectorAll(
                ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done) .agent-chat__thinking-text"
            );
            for (let i = 0; i < cards.length; i++) {
                (cards[i] as HTMLElement).textContent = live;
            }
        };
        tick();
        this.thinkingTimerId = window.setInterval(tick, 100);
    }

    // Stop the thinking timer (called when thinking ends / switching sessions / stopping
    // generation, to avoid leaks).
    private stopThinkingTimer() {
        if (this.thinkingTimerId) {
            clearInterval(this.thinkingTimerId);
            this.thinkingTimerId = 0;
        }
    }

    private finishActiveThinking() {
        this.stopThinkingTimer();
        const L = window.siyuan.languages;
        // The duration is stored as a number (used to persist entry.duration); the "Thought for Xs"
        // text is only shown in the DOM and is not persisted.
        const durSec = this.requestStartTime ? (Date.now() - this.requestStartTime) / 1000 : 0;
        this.currentThinkingDuration = durSec;
        const doneText = durSec > 0
            ? (L.agentThinkingDoneTime ? L.agentThinkingDoneTime.replace("%s", Math.round(durSec) + "s") : (L.agentThinking || "Thinking"))
            : (L.agentThinking || "Thinking");

        const items = this.messagesContainer.querySelectorAll(
            ".agent-chat__msg--thinking:not(.agent-chat__msg--thinking-done)"
        );
        for (let i = 0; i < items.length; i++) {
            const el = items[i] as HTMLElement;
            if (i === items.length - 1) {
                const streamingChat = el.querySelector(".agent-chat__thinking-chat--streaming");
                if (streamingChat) {
                    streamingChat.remove();
                }
            }
            // If the user has not manually interacted and it is still in preview state -> auto-
            // collapse once thinking completes (respecting whatever expand/collapse state the user
            // already set).
            if (!el.hasAttribute("data-user-interacted")) {
                const body = el.querySelector(".agent-chat__thinking-body");
                body?.classList.remove("agent-chat__thinking-body--preview");
            }
            el.classList.add("agent-chat__msg--thinking-done");
            if (doneText) {
                const textEl = el.querySelector(".agent-chat__thinking-text");
                if (textEl) {
                    textEl.textContent = doneText;
                }
            }
        }
    }

    private setStreaming(streaming: boolean) {
        this.isStreaming = streaming;
        this.sendBtn.classList.toggle("fn__none", streaming);
        this.stopBtn.classList.toggle("fn__none", !streaming);
        this.updateSendButtonState();
    }

    // Decide the enabled state of the send button and input box based on a combination of "is
    // streaming", "are there usable models", and "does the input box have content".
    // With no models, also disable both the send button and the input box (disabled attribute +
    // grayed-out style + composer-host disabled state), blocking invalid requests at the source.
    private updateSendButtonState() {
        const disabled = this.isStreaming || this.modelOptions.length === 0 || !this.hasComposerInput();
        if (disabled) {
            this.sendBtn.setAttribute("disabled", "disabled");
        } else {
            this.sendBtn.removeAttribute("disabled");
        }
        if (this.composerHost) {
            // Reuse the existing disabled-state style from streaming (grayed out + blocks
            // interaction).
            // Note: the composer is disabled only while streaming / with no models; an empty input
            // does not disable the composer (the user can still edit normally).
            const composerDisabled = this.isStreaming || this.modelOptions.length === 0;
            this.composerHost.classList.toggle("agent-chat__composer-host--disabled", composerDisabled);
        }
    }

    // Whether the input box currently has sendable content (an @ reference also counts). Returns
    // false when there is no composer.
    private hasComposerInput(): boolean {
        if (!this.composer) {
            return false;
        }
        return this.composer.getSendData().text.length > 0;
    }

    // Keep correcting the scroll position for about duration ms (covering scrollHeight changes
    // during the dock's width/height transition / asynchronous rich rendering), so scrollTop lands
    // at a distance of scrollBottom from the bottom. scrollBottom of 0 means pinned to the bottom.
    // Shared by toggling the panel (layoutVisible recovery) and switching sessions (after
    // renderLoadedSession).
    private restoreScrollToBottom(scrollBottom: number, duration = 320) {
        if (scrollBottom < 0) {
            return;
        }
        const startedAt = Date.now();
        // Mark this as a programmatic scroll, to prevent the scroll event fired during restoration
        // from flipping userScrolledUp.
        this.programmaticScroll = true;
        const tick = () => {
            if (!this.layoutVisible) {
                this.programmaticScroll = false;
                return;
            }
            const {scrollHeight} = this.messagesContainer;
            // The same distance from the bottom; when the distance is 0 (pinned to the bottom),
            // target = scrollHeight.
            const target = Math.max(0, scrollHeight - scrollBottom);
            this.messagesContainer.scrollTop = target;
            if (Date.now() - startedAt < duration) {
                requestAnimationFrame(tick);
            } else {
                // Wait one more frame before clearing the flag, to make sure the last scroll event
                // has been swallowed.
                requestAnimationFrame(() => {
                    this.programmaticScroll = false;
                });
            }
        };
        requestAnimationFrame(tick);
    }

    // After thinking ends, scroll to just below the thinking card: bring the collapsed card's
    // bottom close to the top of the container's viewport, leaving room below it for the
    // about-to-start/already-streaming body. delay waits for the card's collapse max-height
    // transition (~0.2s) to finish.
    private scrollToThinkingCardBelow(card: HTMLElement, delay = 220) {
        const align = () => {
            if (!card.isConnected) {
                return;
            }
            // Use getBoundingClientRect to compute the card bottom's offset relative to the scroll
            // container, avoiding a dependency on whether offsetParent is the scroll container
            // (the positioned ancestor may not be messagesContainer).
            const containerRect = this.messagesContainer.getBoundingClientRect();
            const cardRect = card.getBoundingClientRect();
            // (card bottom's position in the document) - (container top's position in the document)
            // + (current scroll amount) = the target scrollTop for the card's bottom.
            const target = this.messagesContainer.scrollTop + (cardRect.bottom - containerRect.top) + 8;
            const max = this.messagesContainer.scrollHeight - this.messagesContainer.clientHeight;
            this.programmaticScroll = true;
            this.messagesContainer.scrollTop = Math.min(target, max);
            requestAnimationFrame(() => {
                this.programmaticScroll = false;
            });
        };
        if (delay > 0) {
            window.setTimeout(align, delay);
        } else {
            align();
        }
    }

    private scrollToBottom(force = false, smooth = false) {
        if (!force && this.userScrolledUp) {
            return;
        }        // Guard with a flag so the resulting scroll event can be told apart from
        // a user-driven scroll. Without this, the programmatic stick-to-bottom
        // write itself trips the scroll handler and, while streaming, flips
        // userScrolledUp on transiently (scrollHeight keeps growing) which
        // immediately breaks follow-scroll.
        this.programmaticScroll = true;
        requestAnimationFrame(() => {
            if (smooth) {
                // Smooth scrolling fires scroll events asynchronously throughout the
                // animation, so keep the guard raised until scrolling settles: on
                // scrollend, on a 1s timeout fallback, or immediately if the user
                // wheels/touches during the animation (counts as a user scroll).
                const finish = () => {
                    this.messagesContainer.removeEventListener("scrollend", finish);
                    this.messagesContainer.removeEventListener("wheel", onWheel);
                    clearTimeout(timer);
                    this.programmaticScroll = false;
                };
                const onWheel = () => {
                    this.messagesContainer.removeEventListener("scrollend", finish);
                    clearTimeout(timer);
                    this.programmaticScroll = false;
                };
                const timer = window.setTimeout(finish, 1000);
                this.messagesContainer.addEventListener("scrollend", finish, {once: true});
                this.messagesContainer.addEventListener("wheel", onWheel, {once: true, passive: true});
                this.messagesContainer.scrollTo({top: this.messagesContainer.scrollHeight, behavior: "smooth"});
            } else {
                this.messagesContainer.scrollTop = this.messagesContainer.scrollHeight;
                // Reset the flag only after the scroll event caused by this write has
                // been dispatched (a second RAF runs after layout/event delivery).
                requestAnimationFrame(() => {
                    this.programmaticScroll = false;
                });
            }
        });
    }

    // Observe a streaming message card so that asynchronous content growth
    // (code highlighting, images, mermaid, fonts) keeps the view pinned to the
    // bottom while the user has not scrolled up. token frames only fire when a
    // chunk arrives; this closes the gap between chunks.
    private observeStickTarget(el: HTMLElement | null) {
        if (this.stickResizeObserver) {
            this.stickResizeObserver.disconnect();
            this.stickResizeObserver = null;
        }
        if (!el) {
            return;
        }
        this.stickResizeObserver = new ResizeObserver(() => {
            if (!this.userScrolledUp) {
                this.scrollToBottom();
            }
        });
        this.stickResizeObserver.observe(el);
    }

    private toolCategory(name: string): string {
        const L = window.siyuan.languages;
        const m: Record<string, string | undefined> = {
            "block": L.agentCatBlock, "document": L.agentCatDoc,
            "notebook": L.agentCatNotebook, "tag": L.agentCatTag,
            "bookmark": L.agentCatBookmark, "file": L.agentCatFile,
            "asset": L.agentCatAsset, "attr": L.agentCatAttr,
            "dailynote": L.agentCatDailynote, "import": L.agentCatImport,
            "repo": L.agentCatRepo, "history": L.agentCatHistory,
            "sync": L.agentCatSync, "database": L.agentCatDatabase,
        };
        return m[name] || L.agentCatDefault;
    }

    private formatMessageTime(ts: number): string {
        const d = dayjs(ts);
        if (d.format("YYYY-MM-DD") === dayjs().format("YYYY-MM-DD")) {
            return d.format("HH:mm");
        }
        return d.format("YYYY-MM-DD HH:mm");
    }

}
