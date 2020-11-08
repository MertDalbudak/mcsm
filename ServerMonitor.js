const fs = require('fs');
const https = require('https');
const config = require(__dirname + '/config.json');
const exec = require('child_process').exec;
const execSync = require('child_process').execSync;
const CronJob = require('cron').CronJob;
const Handler = require(__dirname + '/handler.js');
const Survey = require(__dirname + '/survey.js');
const download_dir = __dirname + "/" + config.DownloadDir;
const cpu_temp_file = config.CPUTemperature;

module.exports = class {
    constructor(){
        this.server_dir = "/media/autofs/Merts-HDD/paper/";
        this.monitor = {};
        this.handler = new Handler();
        this.temp_policy = require(__dirname + '/temp_policy.js');
        this.event = new (require('../../lib/ngiN-event.js'))();

        fs.watch(this.server_dir + config.ServerLogPath, { encoding: 'utf-8' }, (eventType, filename) => {
            this.event.add("logChange", false, filename);
        });

        this.on('logChange', ()=>{
            this.logLastLines(1, (line)=>{
                console.log(line);
                this.event.add("newLine", false, {'line': line});
                // CHECK IF /SURVEY IS BEING CALLED
                if(line.includes("issued server command: /survey")){
                    if(Survey.ongoing){
                        this.handler.tellraw(line.match("\ ([a-z,A-Z,0-9]*?)\ ")[0].trim(), "A survey is already in process, you cannot start a new one until the ongoing survey lasts.", "red");
                    }
                    else {
                        try{
                            let player = line.match("\\:\\ [^ ]*")[0].substr(2);
                            let options = line.substr(line.indexOf("/survey") + 8);
                            this.createSurvery(player, options.match("\"(.*?)\"")[1], JSON.parse(options.match("\\[(.*?)\\]")[0]), line.match(/\d*\.?\d*$/)[0]);
                        }
                        catch(e){
                            console.error(e);
                        }
                    }
                }
            });
        });
        this.restartCron = new CronJob('0 0 */' + config.ServerRestartInterval + ' * * *', ()=>{
            this.status = "restarting";
            this.handler.tellraw("@a", "Server will restart in 30 seconds", "yellow");
            this.handler.stop(30000, 5000, false, false);
            this.on('restartClosed', ()=>{
                console.log("restart closed", "starting...");
                exec("minecraft", function(err, stdout, stderr){
                    console.log(stdout, "started");
                    exec("n");
                });
            });
        }, null, true, 'Europe/Berlin');
        //this.restartCron.start();
    }
    checkServer(callback){
        exec("if screen -ls | grep -q 'minecraft'; then echo 1; else echo 0; fi",  (err, stdout, stderr)=>{
            if(stdout == 0){
                console.error("Minecraft server is not running anymore", "Exiting...");
                if(this.status != "restarting")
                    process.exit();
                else
                    this.event.add("restartClosed");
            }
            else
                callback();
        });
    }
    isReady(){
        // TODO CHECK WETHER THE SERVER IS READY
    }
    // MONITORING
    temperature(interval_time, temperature_file = cpu_temp_file){
        this.monitor['temperatur'] = setInterval(()=> {
            this.checkServer(() =>{
                    fs.readFile(temperature_file, (err, data)=>{
                        if(err == null){
                            const temp = (data/1000).toFixed(2);
                            if(temp < this.handler.temp_threshold){
                                clearInterval(this.handler.shutdown_interval);
                                clearTimeout(this.handler.shutdown_timeout);
                                exec("screen -S minecraft -X stuff 'say Shutdown aborted.\n'");
                                this.handler.temp_threshold = false;
                                this.handler.stop_block = false;
                            }
                            for(let i = 0; i < this.temp_policy.length; i++){
                                if(temp <= this.temp_policy[i]['max_temp']){
                                    let actions = this.temp_policy[i]["action"](temp);
                                    for(let action in actions){
                                        if(Array.isArray(actions[action]))
                                            this.handler[action](...actions[action]);
                                        else
                                            this.handler[action](actions[action]);
                                    }
                                    break;
                                }
                            }
                        }
                        else{
                            console.error(err);
                            clearInterval(this.monitor['temperatur']);
                        }
                    });
            });
        }, interval_time);
    }

    createSurvery(...args){ 
        if(Survey.ongoing)
            return false;
        let survey = new Survey(...args);
        let onChange = (event)=>{
            let line = event.line;
            console.log(event);
            if(line.match("\<.*\>") == null || line.match("[0-9]+$") == null)
                return false;
            let player_name = line.match("\<.*\>")[0].slice(1,-1), vote = line.match("[0-9]+$")[0];
            survey.vote(player_name, vote);
            survey.on('end', ()=>{
                this.event.removeListener('logChange', onChange);
            });
        };
        this.on('newLine', onChange);
    }

    frequency(interval_time){
        this.monitor['frequency'] = setInterval(()=> {

        }, interval_time);
    }
    checkPaperUpdate(callback){
        console.log("Checking for paper updates...");
        exec("screen -S minecraft -X stuff 'version\n'", ()=>{
            this.event.on("logChange", ()=> {
                const last_line = this.logLastLinesSync(3);
                console.log(last_line);
                callback((last_line.match("[0-9]* version\\(s\\) behind") !== null));
            });
        });
    }
    updateNode(){
        let current_version = this.node_version;
        console.log("Current node version: " + current_version, "Checking for updates...");
        execSync("sudo n stable");
        if(current_version != this.node_version){
            console.log("Updated from node version " + current_version + " to " + this.node_version)
        }
        console.log("Latest node version: " + this.node_version + " installed");
    }
    updatePaper(){
        this.checkPaperUpdate(update_available => {
            if(update_available){
                this.handler.say("There is a paper update available", "Downloading new update...");
                console.log(config.ServerUpdateHost + config.ServerUpdatePath);
                // START DOWNLOAD & INSTALL PROCESS
                this.download(config.ServerUpdateHost + config.ServerUpdatePath, download_dir + "paper-" + Date.now() + ".jar", ()=>{
                    // INSTALL PROCESS
                    this.handler.say("Download complete", "Should the install process start?");
                    console.log("Installing on hold");
                    console.log(execSync("ls -l download_dir").toString().trim());
                });
            }
            else{
                console.log("Latest version of paper already installed");
            }
        });
    }
    download(url, output, callback){
        const download = fs.createWriteStream(output);
        const request = https.get("https://" + url, function(response) {
            response.pipe(download);
        });
        request.on('finish', callback);
    }

    fileReadLastLines(path, n, callback){
        exec("tail -n " + n + " " + path, function(err, stdout, stderr){
            callback(stdout.toString().trim());
        });
    }
    logLastLines(n, callback){
        this.fileReadLastLines(this.server_dir + config.ServerLogPath, n, (lines)=>{
            callback(lines);
        });
    }
    fileReadLastLinesSync(path, n){
        return execSync("tail -n " + n + " " + path).toString().trim();
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.server_dir + config.ServerLogPath, n)
    }
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }

    // GETTER
    get node_version() {
        return execSync("node --version").toString().trim();
    }
};