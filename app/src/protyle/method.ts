import { graphvizRender } from "./render/graphvizRender";
import { highlightRender } from "./render/highlightRender";
import { mathRender } from "./render/mathRender";
import { mermaidRender } from "./render/mermaidRender";
import { flowchartRender } from "./render/flowchartRender";
import { chartRender } from "./render/chartRender";
import { abcRender } from "./render/abcRender";
import { htmlRender } from "./render/htmlRender";
import { mindmapRender } from "./render/mindmapRender";
import { plantumlRender } from "./render/plantumlRender";
import "../assets/scss/export.scss";

class Protyle {
    /** Render graphviz */
    public static graphvizRender = graphvizRender;
    /** Render syntax highlighting for code blocks within an element */
    public static highlightRender = highlightRender;
    /** Render math formulas */
    public static mathRender = mathRender;
    /** Render flowcharts/sequence diagrams/Gantt charts */
    public static mermaidRender = mermaidRender;
    /** Render flowchart.js */
    public static flowchartRender = flowchartRender;
    /** Render charts */
    public static chartRender = chartRender;
    /** Render staff notation */
    public static abcRender = abcRender;
    /** Render mind maps */
    public static mindmapRender = mindmapRender;
    /** Render UML */
    public static plantumlRender = plantumlRender;
    /** Render HTML blocks */
    public static htmlRender = htmlRender;
}

// A temporary workaround for https://github.com/siyuan-note/siyuan/issues/7800
window.Protyle = Protyle;

export default Protyle;
