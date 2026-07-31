// https://github.com/siyuan-note/siyuan/issues/9382
export const nbsp2space = (text: string) => {
    // Convert non-breaking spaces to regular spaces
    return text.replace(/\u00A0/g, " ");
};

// https://github.com/siyuan-note/siyuan/issues/14800
export const removeZWJ = (text: string) => {
    return text.replace(/\u200D```/g, "```");
};
