export const preventScroll = (protyle: IProtyle, scrollTop = 0, timeout = 1000) => {
    // Prevent a get request from being triggered after the scrollbar scrolls
    protyle.scroll.lastScrollTop = -1;
    setTimeout(() => {
        protyle.scroll.lastScrollTop = scrollTop;
    }, timeout);
};
