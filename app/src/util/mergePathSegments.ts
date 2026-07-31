/**
 * Merges relative path segments into an existing segments array: `..` goes up one level (no
 * further up once already at the root), and `.` and empty segments are ignored.
 * Modifies `pathSegments` in place and returns the same array.
 */
export const mergePathSegments = (pathSegments: string[], segments: string[]): string[] => {
    for (const segment of segments) {
        if (segment === "..") {
            if (pathSegments.length > 0) {
                pathSegments.pop();
            }
        } else if (segment && segment !== ".") {
            pathSegments.push(segment);
        }
    }
    return pathSegments;
};
