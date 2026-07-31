// The Lute config entirely reads from the global window.siyuan.config / window.siyuan.emojis, which
// is consistent across editors, so all Protyle editors share the same Lute instance, reducing memory
// and initialization overhead from O(number of editors) to O(1).
// AgentChat does not reuse this shared singleton; instead it builds an independent instance via
// getAgentLute, so its rendering is unaffected by editor settings.
let luteInstance: Lute | undefined;

/**
 * Gets the shared Lute singleton (creating it on first call).
 *
 * options are only applied on first creation; subsequent calls directly return the cached instance —
 * the Lute config is inherently derived from the global config and is consistent across editors, so
 * there's no need to distinguish by editor.
 */
export const getLute = (options: ILuteOptions): Lute => {
    if (!luteInstance) {
        luteInstance = setLute(options);
    }
    return luteInstance;
};

/**
 * Directly gets the already-initialized shared Lute singleton.
 * Used for scenarios like emoji that don't need to pass in options; returns undefined if not yet created.
 */
export const getLuteInstance = (): Lute | undefined => {
    return luteInstance;
};

/**
 * Builds an independent Lute instance for the agent (AgentChat).
 *
 * Unlike the shared singleton: it does not read the syntax toggles from
 * window.siyuan.config.editor.markdown, but instead hardcodes all Markdown inline syntax (italic,
 * bold, strikethrough, superscript/subscript, tags, inline formulas, marks) as enabled, so LLM output
 * is always rendered as standard Markdown, unaffected by the user's "Editor -> Markdown syntax
 * settings". Each call returns a new instance, isolated from editor rendering.
 */
export const getAgentLute = (options: ILuteOptions): Lute => {
    const lute: Lute = Lute.New();
    lute.SetSpellcheck(false);
    lute.SetProtyleMarkNetImg(false);
    lute.SetFileAnnotationRef(true);
    lute.SetHTMLTag2TextMark(true);
    lute.SetTextMark(true);
    lute.SetHeadingID(false);
    lute.SetYamlFrontMatter(false);
    lute.PutEmojis(options.emojis);
    lute.SetEmojiSite(options.emojiSite);
    lute.SetHeadingAnchor(options.headingAnchor);
    lute.SetInlineMathAllowDigitAfterOpenMarker(true);
    lute.SetToC(false);
    lute.SetIndentCodeBlock(false);
    lute.SetParagraphBeginningSpace(true);
    lute.SetSetext(false);
    lute.SetFootnotes(false);
    lute.SetLinkRef(false);
    lute.SetSanitize(options.sanitize);
    lute.SetChineseParagraphBeginningSpace(options.paragraphBeginningSpace);
    lute.SetRenderListStyle(options.listStyle);
    lute.SetImgPathAllowSpace(true);
    lute.SetKramdownIAL(true);
    lute.SetSuperBlock(true);
    lute.SetCallout(true);
    // All inline syntax is enabled, independent of editor settings.
    lute.SetInlineAsterisk(true);
    lute.SetInlineUnderscore(true);
    lute.SetSup(true);
    lute.SetSub(true);
    lute.SetTag(true);
    lute.SetInlineMath(true);
    lute.SetGFMStrikethrough1(false);
    lute.SetGFMStrikethrough(true);
    lute.SetMark(true);
    lute.SetSpin(true);
    lute.SetProtyleWYSIWYG(true);
    if (options.lazyLoadImage) {
        lute.SetImageLazyLoading(options.lazyLoadImage);
    }
    lute.SetBlockRef(true);
    lute.SetUnorderedListMarker("-");
    lute.SetDataTask(true);
    lute.SetExportNormalizeTaskListMarker(true);
    lute.SetArbitraryTaskListItemMarker(true);
    lute.SetEnsureListItemParagraph(true);
    return lute;
};

/**
 * Builds a new Lute instance based on the global config and the given options, used to initialize the shared singleton.
 */
const setLute = (options: ILuteOptions) => {
    const lute: Lute = Lute.New();
    lute.SetSpellcheck(window.siyuan.config.editor.spellcheck);
    lute.SetProtyleMarkNetImg(window.siyuan.config.editor.displayNetImgMark);
    lute.SetFileAnnotationRef(true);
    lute.SetHTMLTag2TextMark(true);
    lute.SetTextMark(true);
    lute.SetHeadingID(false);
    lute.SetYamlFrontMatter(false);
    lute.PutEmojis(options.emojis);
    lute.SetEmojiSite(options.emojiSite);
    lute.SetHeadingAnchor(options.headingAnchor);
    lute.SetInlineMathAllowDigitAfterOpenMarker(true);
    lute.SetToC(false);
    lute.SetIndentCodeBlock(false);
    lute.SetParagraphBeginningSpace(true);
    lute.SetSetext(false);
    lute.SetFootnotes(false);
    lute.SetLinkRef(false);
    lute.SetSanitize(options.sanitize);
    lute.SetChineseParagraphBeginningSpace(options.paragraphBeginningSpace);
    lute.SetRenderListStyle(options.listStyle);
    lute.SetImgPathAllowSpace(true);
    lute.SetKramdownIAL(true);
    lute.SetTag(true);
    lute.SetSuperBlock(true);
    lute.SetCallout(true);
    lute.SetInlineAsterisk(window.siyuan.config.editor.markdown.inlineAsterisk);
    lute.SetInlineUnderscore(window.siyuan.config.editor.markdown.inlineUnderscore);
    lute.SetSup(window.siyuan.config.editor.markdown.inlineSup);
    lute.SetSub(window.siyuan.config.editor.markdown.inlineSub);
    lute.SetTag(window.siyuan.config.editor.markdown.inlineTag);
    lute.SetInlineMath(window.siyuan.config.editor.markdown.inlineMath);
    lute.SetGFMStrikethrough1(false);
    lute.SetGFMStrikethrough(window.siyuan.config.editor.markdown.inlineStrikethrough);
    lute.SetMark(window.siyuan.config.editor.markdown.inlineMark);
    lute.SetSpin(true);
    lute.SetProtyleWYSIWYG(true);
    if (options.lazyLoadImage) {
        lute.SetImageLazyLoading(options.lazyLoadImage);
    }
    lute.SetBlockRef(true);
    if (window.siyuan.emojis[0].items.length > 0) {
        const emojis: IObject = {};
        window.siyuan.emojis[0].items.forEach(item => {
            emojis[item.keywords] = options.emojiSite + "/" + item.unicode;
        });
        lute.PutEmojis(emojis);
    }
    lute.SetUnorderedListMarker("-");
    lute.SetDataTask(true);
    lute.SetExportNormalizeTaskListMarker(true);
    lute.SetArbitraryTaskListItemMarker(true);
    lute.SetEnsureListItemParagraph(true); // Add an empty paragraph before creating a sub-list under an empty list item
    return lute;
};
