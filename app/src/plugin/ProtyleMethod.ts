import { graphvizRender } from "../protyle/render/graphvizRender";
import { highlightRender } from "../protyle/render/highlightRender";
import { mathRender } from "../protyle/render/mathRender";
import { mermaidRender } from "../protyle/render/mermaidRender";
import { flowchartRender } from "../protyle/render/flowchartRender";
import { chartRender } from "../protyle/render/chartRender";
import { abcRender } from "../protyle/render/abcRender";
import { htmlRender } from "../protyle/render/htmlRender";
import { mindmapRender } from "../protyle/render/mindmapRender";
import { plantumlRender } from "../protyle/render/plantumlRender";
import { avRender } from "../protyle/render/av/render";

export class ProtyleMethod {
    /** Render graphviz */
    public static graphvizRender = graphvizRender;
    /** Render syntax highlighting for code blocks in an element */
    public static highlightRender = highlightRender;
    /** Render math formulas */
    public static mathRender = mathRender;
    /** Render flowcharts/sequence diagrams/Gantt charts */
    public static mermaidRender = mermaidRender;
    /** flowchart.js rendering */
    public static flowchartRender = flowchartRender;
    /** Render charts */
    public static chartRender = chartRender;
    /** Render sheet music */
    public static abcRender = abcRender;
    /** Render mind maps */
    public static mindmapRender = mindmapRender;
    /** Render UML */
    public static plantumlRender = plantumlRender;
    public static avRender = avRender;
    public static htmlRender = htmlRender;
}
