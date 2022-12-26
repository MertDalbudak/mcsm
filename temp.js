const fs = require('fs');
const {get} = require('https');
const exec = require('child_process').exec;
const execSync = require('child_process').execSync;
const CronJob = require('cron').CronJob;
const config = require(__dirname + '/config.json');
const Server = require(__dirname + '/Server/' + config.ServerType);
const Handler = require(__dirname + '/handler.js');

config.ServerExecutable = Server.ServerExecutable;
config.ServerUpdateHost = Server.ServerUpdateHost;
config.ServerUpdatePath = Server.ServerUpdatePath;

module.exports = class {
    constructor(){
        this.server_dir = config.ServerPath;
        this.monitor = {};
        this.handler = new Handler();
        this.temp_policy = require(__dirname + '/temp_policy.js');
        this.event = new (require('events'));

        this.newVersionAvailable = Server.newVersionAvailable;

        fs.watch(this.server_dir + config.ServerLogPath, { encoding: 'utf-8' }, (eventType, filename) => {
            this.event.emit("logChange", filename);
        });

        fs.watch(this.server_dir + '/ops.json', { encoding: 'utf-8' }, (eventType, filename) => {
            this.event.emit("opsChange", filename);
        });

        this.on('logChange', ()=>{
            this.logLastLines(1, (line)=>{
                console.log(line);
                let parsed_line = this.parseLine(line);
                if(parsed_line != null)
                    this.event.emit("newLogLine", parsed_line);
            });
        });

        this.updateCron();
        this.restartCron();
        this.checkServer();
    }
    parseLine(line){
        let timestamp = line.match(/^\[[0-9]*:[0-9]*:[0-9]* [A-Z]*\]:/);
        if(timestamp == null)
            return null;
        let parsed_line = {};
        parsed_line.content = line.substr(timestamp[0].length).trim();
        parsed_line.date = new Date(Date.UTC());
        parsed_line.initiator = parsed_line.content.match(/\<.*\>|\[Server\]/).slice(1,-1); // STRING (NAME OF PLAYER)
        parsed_line.message = parsed_line.initiator != null ? parsed_line.content.substr(parsed_line.initiator.length + 2).trim() : null;


        return parsed_line;
    }
    restartCron(){
        this.restart_cron = new CronJob(config.ServerRestartInterval, ()=>{
            if(this.status != "running")
                return false;
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
        this.restart_cron.start();
    }
    updateCron(){
        this.update_cron = new CronJob('* * * * *', ()=> {this.update()}, null, true, 'Europe/Berlin');
        //this.update_cron = new CronJob(config.ServerCheckUpdateInterval, this.update, null, true, 'Europe/Berlin');
        this.update_cron.start();
    }
    checkServer(callback){
        exec("if screen -ls | grep -q 'minecraft'; then echo 1; else echo 0; fi",  (err, stdout, stderr)=>{
            if(stdout == 0){
                if(this.status != "restarting" && this.status != "updating"){
                    console.error("Minecraft server is not running anymore", "Exiting...");
                    process.exit();
                }
                else{
                    this.event.emit("restartClosed");
                    if(callback)
                        callback(false);
                }
            }
            else{
                this.status = "running";
                if(callback){
                    callback(true);
                }
            }
        });
    }
    isReady(){
        // TODO CHECK WETHER THE SERVER IS READY
    }
    // MONITORING
    temperature(interval_time){
        this.monitor['temperatur'] = setInterval(()=> {
            this.checkServer();
            fs.readFile(config.CPUTemperature, (err, data)=>{
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
        }, interval_time);
    }
    frequency(interval_time){
        this.monitor['frequency'] = setInterval(()=> {
            fs.readFile(this.config.CPUFrequency, (err, data)=>{
                if(err == null){
                    exec(`screen -S minecraft -X stuff 'say Server CPU frequency is ${data.trim()}.\n'`);
                }
                else{
                    console.error(err);
                    clearInterval(this.monitor['frequency']);
                }
            });
        }, interval_time);
    }
    async update(){
        console.log(await this.newVersionAvailable());
        if(this.status != "running")
            return false;
        if(await this.newVersionAvailable() == false){
            console.log("Already running on the latest version")
            return false;
        }
        // ASK FOR PERMITION TO UPDATE PAPER ( ONLY OP's CAN CONFIRM )
        this.ops.forEach(player => {
            this.handler.tellraw(player, "A new version of paper is available, do you want to install the new Version? (Yes, No)", "yellow");
        });

        let confirm_update = (line)=>{
            if(this.ops.findIndex(line.initiator) != -1){
                if(this.status != "running")
                    return;
                if(line.message == 'Yes'){
                    this.status = "updating";
                    this.event.removeListener('newLogLine', confirm_update);
                    this.handler.tellraw(line.initiator, "Downloading new update...", "green");
                    this.handler.tellraw("@a", "Server will be updated. The server will shutdown in 30 seconds and will come back as soon as the update is completed.", "red");
                    this.handler.stop(30000, 5000, false, false).then(()=> {
                        // DOWNLOAD SERVER FILES
                        this.download(config.ServerUpdateHost + config.ServerUpdatePath, config.ServerPath + config.DownloadDir + "paper-" + Date.now() + ".jar", (output_file)=>{
                            // INSTALL PROCESS
                            console.log("Installing new version of minecraft...");
                            console.log(execSync("ls -l config.DownloadDir").toString().trim());
                            fs.unlinkSync(config.ServerPath + config.ServerExecutable);
                            fs.renameSync(output_file, config.ServerPath + config.ServerExecutable);
                            this.startServer();
                        });
                    });
                }
                else{
                    this.handler.tellraw(line.initiator, "Not updating", "black");
                }
            }
        };
        this.on('newLogLine', confirm_update);
    }
    download(url, output_file, callback){
        const download = fs.createWriteStream(output);
        const request = get("https://" + url, function(response) {
            response.pipe(download);
        });
        request.on('finish', ()=> callback(output_file));
    }

    fileReadLastLines(path, n, callback){
        exec("tail -n " + n + " " + path, function(err, stdout, stderr){
            callback(stdout.toString().trim());
        });
    }
    fileReadLastLinesSync(path, n){
        return execSync("tail -n " + n + " " + path).toString().trim();
    }
    logLastLines(n, callback){
        this.fileReadLastLines(this.server_dir + config.ServerLogPath, n, (lines)=>{
            callback(lines);
        });
    }
    logLastLinesSync(n){
        return this.fileReadLastLinesSync(this.server_dir + config.ServerLogPath, n)
    }
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }

    startServer(){
        this.checkServer((state)=>{
            if(state == false)
                exec(`screen -dmS minecraft bash -c 'cd ${config.ServerPath} && java -Xms2G -Xmx3G -XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch -XX:G1NewSizePercent=30 -XX:G1MaxNewSizePercent=40 -XX:G1HeapRegionSize=8M -XX:G1ReservePercent=20 -XX:G1HeapWastePercent=5 -XX:G1MixedGCCountTarget=4 -XX:InitiatingHeapOccupancyPercent=15 -XX:G1MixedGCLiveThresholdPercent=90 -XX:G1RSetUpdatingPauseTimePercent=5 -XX:SurvivorRatio=32 -XX:+PerfDisableSharedMem -XX:MaxTenuringThreshold=1 -jar ${config.ServerExecutable} nogui'`)
        });
    }

    // GETTER
    get node_version() {
        return execSync("node --version").toString().trim();
    }

    get ops() {
        return require(`${config.ServerPath}/ops.json`).map(e => e.name);
    }
};