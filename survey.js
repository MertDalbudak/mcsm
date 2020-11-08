const Handler = require(__dirname + '/handler.js');
const handler = new Handler();

class Survey {
    constructor(initiator, msg, options, timeout = 1, answere = null){
        this.id = ++Survey.created;
        this.msg = msg;
        this.options = options;
        this.options_msg = this.options.reduce((acc, curr, index) => {
            acc.push((index + 1) + ": " + curr);
            return acc;
        }, new Array());
        this.timeout = Math.min((timeout * 1000 * 60), 300000); // SURVEY LAST MAX. 5 MINS
        this.answere = answere;
        this.participated_players = [];
        this.status = "running";
        this.initiator = initiator;
        this.event = new (require('../../lib/ngiN-event.js'))();
        this.time_indication = this.timeout < 60000 ? (this.timeout / 1000).toFixed(1) + " seconds" : Math.ceil(this.timeout / 60000).toFixed(1) + " minutes";
            
        
        handler.tellraw("@a", this.initiator + " started a new survey:", "green", ()=>{
            handler.tellraw("@a", this.msg, "yellow", ()=>{
                handler.saySync(...this.options_msg);
                handler.tellraw("@a", "Survey will end in " + this.time_indication + ". Vote now!", "red");
            });
        });
        setTimeout(()=> {
            handler.saySync("Survey ended!", this.participated_players.length + " player participated");
            const result = this.result();
            if(result != false){
                handler.tellraw("@a", "Most voted option is: " + this.options[result.index] + ". By " + result.total + " vote(s).", "green", ()=>{
                    handler.tellraw("@a", "Thanks to everyone who participated", "red");
                });
            }
            this.status = "ended";
            Survey.ongoing = false;
            this.event.add('end', true);
        }, this.timeout);
        Survey.ongoing = true;
    }
    result(){
        let most_votes = {'index': 0, 'total': 0};
        for(let i = 0; i < this.votes.length; i++){
            if(this.votes[i] > most_votes.total){
                most_votes.index = i;
                most_votes.total = this.votes[i];
            }
        }
        if(most_votes.total > 0)
            return most_votes;
        return false;
    }
    vote(player_name, vote){
        vote = parseInt(vote);
        if(isNaN(vote)) // IS NOT A NUMBER NOR A INTEGER
            return false;
        vote -= 1;
        if(vote < 0 || vote >= this.options.length){
            handler.msg(player_name, "Invalid answere. Type the number infront of your answere");
            return false;
        }
        let player_index = this.participated_players.findIndex(element => element.player_name == player_name);
        if(player_index > -1){  // CHECK IF ALREADY PARTICIPATED
            this.participated_players[player_index]['vote'] = vote;
            handler.msg(player_name, "You changed your vote to: " + this.options[vote]);
        }
        else{
            this.participated_players.push({'player_name': player_name, 'vote': vote});
            handler.msg(player_name, "Thank you for your participation. You voted for: " + this.options[vote]);
        }
    }
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    get votes(){
        let votes = new Array(this.options.length).fill(0);
        for(let i = 0; i < this.participated_players.length; i++){
            votes[this.participated_players[i]['vote']] += 1;
        }
        return votes;
    }
}

Survey.created = 0;
Survey.ongoing = false;
module.exports = Survey;