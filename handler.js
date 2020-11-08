const {exec, execSync} = require('child_process');

module.exports = class {
    constructor(){
        this.shutdown_timeout = 0;
        this.shutdown_interval = 0;
        this.restart_timeout = 0;
        this.restart_interval = 0;
        this.temp_threshold = false;
        this.stop_block = false;
    }

    // SHUTDOWN SERVER AND EXITS
    kill(kill_all = true){
        exec("screen -S minecraft -X stuff 'stop\n'", (err, stdout, stderr)=> {
            if(kill_all)
                process.exit();
        });
    }

    // SHUTDOWN WITH
    stop(timeout, announce_server = false, temp_threshold = true, kill = true){
        if(typeof timeout != "number"){
            console.error("First parameter must be number, " + typeof timeout + " given.");
            return false;
        }
        if(this.stop_block){
            console.warn("Ongoing server stop in process cannot stop until it's aborted");
            return false;   
        }
        this.temp_threshold = temp_threshold;
        if(announce_server !== false) { // tell everonye that server is now shutting down
            let time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
            this.shutdown_interval = setInterval(()=> {
                timeout -= announce_server;
                time_indication = timeout < 60000 ? Math.ceil(timeout / 1000) + " seconds" : (timeout < 3600000 ? Math.ceil(timeout / 60000) + " minutes" : Math.ceil(timeout / 3600000) + " hours");
                if(timeout > 0)
                    this.say("Shutting down in " + time_indication);
                else {
                    clearInterval(this.shutdown_interval);
                }
            }, announce_server);
            this.say("Shutting down in " + time_indication);
        }
        this.log("Stopping server in " + (timeout / 1000) + " seconds");
        this.shutdown_timeout = setTimeout(()=>{
            this.kill(kill);
        }, timeout);
        this.stop_block = true;
        return true;
    }

    // LOG
    log(...msg){
        console.log(...msg);
    }

    // SERVER ECHO
    say(msg, callback = null){
        if(msg == null)
            return false;
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'say " + msg + "\n'", function(...args){
            if(callback != null)
                callback();
        });
    }
    saySync(...msg){
        if(msg == null)
            return false;
        if(Array.isArray(msg)){
            for(let i = 0; i < msg.length; i++){
                msg[i] = msg[i].replace("\n", '\\n'); // Escape \n
                execSync("screen -S minecraft -X stuff 'say " + msg[i] + "\n'");
            }
        }
        else{
            msg = msg.replace("\n", '\\n'); // Escape \n
            execSync("screen -S minecraft -X stuff 'say " + msg + "\n'");
        }
        
    }

    tellraw(player, msg, color, callback = null){
        if(msg == null)
            return false;
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'tellraw " + player + " [{\"text\":\"" + msg + "\",\"color\":\"" + color + "\"}]\n'", function(...args){
            if(callback != null)
                callback();
        });
    }

    // MESSAGE PLAYER
    msg(player_name, msg){
        msg = msg.replace("\n", '\\n'); // Escape \n
        exec("screen -S minecraft -X stuff 'msg " + player_name + " " + msg + "\n'");
    }
}