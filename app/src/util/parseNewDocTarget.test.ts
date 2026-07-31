import {describe, it} from "node:test";
import * as assert from "node:assert/strict";
import {getNewDocTargetFromSavePath, getNewDocTargetFromTree, NewDocTarget} from "./parseNewDocTarget";

const assertSubDoc = (target: NewDocTarget, expected: {
    targetNotebookId?: string;
    parentPath: string;
    title: string;
}) => {
    assert.equal(target.kind, "subDoc");
    if (target.kind === "subDoc") {
        if (expected.targetNotebookId !== undefined) {
            assert.equal(target.targetNotebookId, expected.targetNotebookId);
        }
        assert.equal(target.parentPath, expected.parentPath);
        assert.equal(target.title, expected.title);
    }
};

const assertHPath = (target: NewDocTarget, expected: {
    targetNotebookId?: string;
    hPath: string;
    title: string;
}) => {
    assert.equal(target.kind, "hPath");
    if (target.kind === "hPath") {
        if (expected.targetNotebookId !== undefined) {
            assert.equal(target.targetNotebookId, expected.targetNotebookId);
        }
        assert.equal(target.hPath, expected.hPath);
        assert.equal(target.title, expected.title);
    }
};

describe("getNewDocTargetFromSavePath", () => {
    // Focused nested document: kernel path and human path come as a pair
    const nestedDocPath = "/20260628041644-ndcuikw/20260628040939-kkaajwr.sy";
    const nestedHPath = "/parent1/parent2/docName";
    // Focused root-level document
    const rootDocPath = "/20260628041702-kqfrg7p.sy";
    const rootHPath = "/docName";
    const notebookId = "nb";

    const nestedContext = {
        hPath: nestedHPath,
        targetNotebookId: notebookId,
        currentNotebookId: notebookId,
        hasFocusTarget: true,
        currentPath: nestedDocPath,
    };

    describe("empty template", () => {
        it("has tab/selection + no name -> subdoc under the current document", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: ""});
            assertSubDoc(target, {targetNotebookId: notebookId, parentPath: nestedDocPath, title: ""});
        });

        it("has tab/selection + explicit title -> create by name under the current hPath", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "", name: "docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/docName2", title: "docName2"});
        });

        it("focused on a root-level document + no name -> subdoc", () => {
            const target = getNewDocTargetFromSavePath({
                ...nestedContext,
                templatePath: "",
                hPath: rootHPath,
                currentPath: rootDocPath,
            });
            assertSubDoc(target, {parentPath: rootDocPath, title: ""});
        });

        it("focused on a root-level document + explicit title -> create by name under the current hPath", () => {
            const target = getNewDocTargetFromSavePath({
                ...nestedContext,
                templatePath: "",
                hPath: rootHPath,
                currentPath: rootDocPath,
                name: "docName2",
            });
            assertHPath(target, {hPath: "/docName/docName2", title: "docName2"});
        });

        it("notebook root selected (currentPath=/) + no name -> subdoc", () => {
            const target = getNewDocTargetFromSavePath({
                ...nestedContext,
                templatePath: "",
                hPath: "/",
                currentPath: "/",
            });
            assertSubDoc(target, {parentPath: "/", title: ""});
        });

        it("no tab and no selection -> notebook root", () => {
            const target = getNewDocTargetFromSavePath({
                ...nestedContext,
                templatePath: "",
                hasFocusTarget: false,
                hPath: "/",
                currentPath: "/",
            });
            assertHPath(target, {hPath: "/", title: ""});
        });

        it("no tab and no selection + explicit title -> create by name under the notebook root", () => {
            const target = getNewDocTargetFromSavePath({
                ...nestedContext,
                templatePath: "",
                hasFocusTarget: false,
                hPath: "/",
                name: "docName2",
            });
            assertHPath(target, {hPath: "/docName2", title: "docName2"});
        });
    });

    describe("container path (trailing /)", () => {
        it("absolute /parent3/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/parent3/"});
            assertHPath(target, {hPath: "/parent3/", title: ""});
        });

        it("absolute /parent3/ + explicit title", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/parent3/", name: "docName2"});
            assertHPath(target, {hPath: "/parent3/docName2", title: "docName2"});
        });

        it("absolute /", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/", hPath: "/"});
            assertHPath(target, {hPath: "/", title: ""});
        });

        it("absolute /parent1/parent2/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/parent1/parent2/"});
            assertHPath(target, {hPath: "/parent1/parent2/", title: ""});
        });

        it("relative parent3/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "parent3/"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/parent3/", title: ""});
        });

        it("relative parent3/parent4/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "parent3/parent4/"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/parent3/parent4/", title: ""});
        });

        it("relative ../", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../"});
            assertHPath(target, {hPath: "/parent1/parent2/", title: ""});
        });

        it("relative ../ + explicit title", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../", name: "docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/docName2", title: "docName2"});
        });

        it("relative ../../", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../../"});
            assertHPath(target, {hPath: "/parent1/", title: ""});
        });

        it("relative ../parent3/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../parent3/"});
            assertHPath(target, {hPath: "/parent1/parent2/parent3/", title: ""});
        });

        it("relative ../../parent3/parent4/", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../../parent3/parent4/"});
            assertHPath(target, {hPath: "/parent1/parent3/parent4/", title: ""});
        });

        it("already at root, ../", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../", hPath: "/"});
            assertHPath(target, {hPath: "/", title: ""});
        });

        it("trims leading/trailing whitespace in the template", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "  parent3/  "});
            assertHPath(target, {hPath: "/parent1/parent2/docName/parent3/", title: ""});
        });
    });

    describe("document name path", () => {
        it("relative docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/docName2", title: "docName2"});
        });

        it("relative docName2 + explicit title replaces the last segment", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "docName2", name: "docName3"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/docName3", title: "docName3"});
        });

        it("trims leading/trailing whitespace in the template", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "  docName2  "});
            assertHPath(target, {hPath: "/parent1/parent2/docName/docName2", title: "docName2"});
        });

        it("absolute /docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/docName2"});
            assertHPath(target, {hPath: "/docName2", title: "docName2"});
        });

        it("absolute /docName2 + explicit title replaces the last segment", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/docName2", name: "docName3"});
            assertHPath(target, {hPath: "/docName3", title: "docName3"});
        });

        it("relative parent3/docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "parent3/docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/docName/parent3/docName2", title: "docName2"});
        });

        it("absolute /parent3/docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "/parent3/docName2"});
            assertHPath(target, {hPath: "/parent3/docName2", title: "docName2"});
        });

        it("relative ../docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/docName2", title: "docName2"});
        });

        it("relative ../../docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../../docName2"});
            assertHPath(target, {hPath: "/parent1/docName2", title: "docName2"});
        });

        it("relative ../parent3/docName2", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../parent3/docName2"});
            assertHPath(target, {hPath: "/parent1/parent2/parent3/docName2", title: "docName2"});
        });

        it("already at root, ../docName2 (.. has no effect at the root)", () => {
            const target = getNewDocTargetFromSavePath({...nestedContext, templatePath: "../docName2", hPath: "/"});
            assertHPath(target, {hPath: "/docName2", title: "docName2"});
        });
    });

    describe("cross-notebook", () => {
        const crossNotebook = {
            ...nestedContext,
            targetNotebookId: "box-b",
            currentNotebookId: "box-a",
            hPath: "/",
            currentPath: nestedDocPath,
        };

        it("relative docName2 -> pad with / then resolve against the target notebook root", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: "docName2"});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/docName2", title: "docName2"});
        });

        it("relative parent3/parent4/ -> pad with / then resolve against the target notebook root", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: "parent3/parent4/"});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/parent3/parent4/", title: ""});
        });

        it("relative ../docName2 -> pad with /, .. has no effect at the root", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: "../docName2"});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/docName2", title: "docName2"});
        });

        it("absolute /parent3/docName2 is unaffected", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: "/parent3/docName2"});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/parent3/docName2", title: "docName2"});
        });

        it("empty template + no tab and no selection + explicit title -> create by name under the target notebook root", () => {
            const target = getNewDocTargetFromSavePath({
                ...crossNotebook,
                templatePath: "",
                hasFocusTarget: false,
                name: "docName2",
            });
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/docName2", title: "docName2"});
        });

        it("empty template + focused + no name -> falls back to an empty-title document at the target notebook root", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: ""});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/", title: ""});
        });

        it("empty template + focused + explicit title -> create by name under the target notebook root (cross-notebook hPath base is /)", () => {
            const target = getNewDocTargetFromSavePath({...crossNotebook, templatePath: "", name: "docName2"});
            assertHPath(target, {targetNotebookId: "box-b", hPath: "/docName2", title: "docName2"});
        });
    });
});

describe("getNewDocTargetFromTree", () => {
    const notebookId = "nb";
    // Doc tree data-path: kernel path with .sy
    const parentDocPath = "/20260628041644-ndcuikw.sy";
    const nestedDocPath = "/20260628041644-ndcuikw/20260628040939-kkaajwr.sy";
    // pathPosix().dirname(): the parent directory passed in for a sibling insert (no .sy, no trailing slash)
    const parentDirPath = "/20260628041644-ndcuikw";
    const rootPath = "/";

    describe("doc tree + new subdocument (currentPath is a data-path)", () => {
        const treeContext = {currentNotebookId: notebookId, currentPath: parentDocPath};

        it("empty template", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: ""});
            assertSubDoc(target, {targetNotebookId: notebookId, parentPath: parentDocPath, title: ""});
        });

        it("empty template + explicit title", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "", name: "docName2"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("single-segment template docName2", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "docName2"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("single-segment template docName2 + explicit title", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "docName2", name: "docName3"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName3"});
        });

        it("multi-segment template parent3/docName2 (only the last segment becomes the title, parent path unchanged)", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "parent3/docName2"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("absolute template /docName2 (the target is still currentPath, only the title is affected)", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "/docName2"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("container template docName2/ (the tree entry point does not resolve container chains; empty title when there is no name)", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "docName2/"});
            assertSubDoc(target, {parentPath: parentDocPath, title: ""});
        });

        it("container template docName2/ + explicit title", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "docName2/", name: "docName2"});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("trims leading/trailing whitespace in the template", () => {
            const target = getNewDocTargetFromTree({...treeContext, templatePath: "  docName2  "});
            assertSubDoc(target, {parentPath: parentDocPath, title: "docName2"});
        });

        it("create a new subdocument under a nested document", () => {
            const target = getNewDocTargetFromTree({
                currentNotebookId: notebookId,
                currentPath: nestedDocPath,
                templatePath: "docName2",
            });
            assertSubDoc(target, {parentPath: nestedDocPath, title: "docName2"});
        });
    });

    describe("sibling insert (currentPath is a dirname, no .sy)", () => {
        it("sibling insert for a nested document", () => {
            const target = getNewDocTargetFromTree({
                currentNotebookId: notebookId,
                currentPath: parentDirPath,
                templatePath: "docName2",
            });
            assertSubDoc(target, {parentPath: parentDirPath, title: "docName2"});
        });

        it("sibling insert for a root-level document (dirname is /)", () => {
            const target = getNewDocTargetFromTree({
                currentNotebookId: notebookId,
                currentPath: rootPath,
                templatePath: "",
            });
            assertSubDoc(target, {parentPath: rootPath, title: ""});
        });
    });

    describe("notebook root (currentPath is /)", () => {
        it("empty template", () => {
            const target = getNewDocTargetFromTree({
                currentNotebookId: notebookId,
                currentPath: rootPath,
                templatePath: "",
            });
            assertSubDoc(target, {parentPath: rootPath, title: ""});
        });

        it("single-segment template + explicit title", () => {
            const target = getNewDocTargetFromTree({
                currentNotebookId: notebookId,
                currentPath: rootPath,
                templatePath: "docName2",
                name: "docName3",
            });
            assertSubDoc(target, {parentPath: rootPath, title: "docName3"});
        });
    });
});
