const ServerMonitor = require(__dirname + "/ServerMonitor.js");

const app = new ServerMonitor();

// HEARTBEAT OF CHECKING THE VITAL PARAMETERS OF THE SERVER
const check_interval = 15000; // time in ms


function main(){
	// CHECK IF NEW NODE JS VERSION IS AVAILABLE
	//app.updateNode();
	//app.updatePaper();

	// Monitor CPU temperature
	app.temperature(check_interval);

	//app.createSurvery("Welche Farbe ist der Himmel?", ["Rot", "Grau", "Blau", "Schwarz"], 15000);
}

main();
