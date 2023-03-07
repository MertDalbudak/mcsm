const { Client, Collection, Events, GatewayIntentBits, REST, Routes, SlashCommandBuilder  } = require('discord.js');
const config = require("./config.json");
const Event = require('events')

let supported_commands = [
    {
        'name': "help",
        'description': "Lists all available commands",
        'reply': (issuer)=> `Following commands are available:\n${supported_commands.filter(e => e.active).map(e => `> ${e.name}`).join('\r\n')}`,
        'active': false
    },
    {
        'name': "server",
        'description': "Gets server data",
        'reply': (issuer)=> null,
        'active': false
    },
    {
        'name': "list",
        'description': "Lists the online player count and their names",
        'reply': (issuer)=> null,
        'active': false
    },
    {
        'name': "version",
        'description': "Shows the version which the server is currently running at",
        'reply': (issuer)=> `Checking server version...`,
        'active': false
    },
    {
        'name': "temp",
        'description': "Shows the last fetched temperature of the running server",
        'reply': (issuer)=> `Checking server temperature...`,
        'active': false
    },
    {
        'name': "banlist",
        'description': "List all players that have been banned from the Minecraft server",
        'reply': (issuer)=> `Getting list of banned players...`,
        'active': false
    }
];


module.exports = class{
    constructor(server_id){
        this.server_config = config.Servers.find(e => e.id == server_id);
        if(this.server_config == undefined){
            throw new Error(`Couldn't couldn't find server in config file (Id: ${server_id})`);
        }
        this.config = this.server_config.discord;
        this.commands_obeyed_counter = 0;
        this.guilds = [];
        this.rest = new REST({ version: '10' }).setToken(this.config.token);
        this.client = new Client({
            intents: [
                GatewayIntentBits.Guilds,
                GatewayIntentBits.GuildMessages,
                GatewayIntentBits.MessageContent
            ]
        });
        this.client.login(this.config.token);
        this.client.commands = new Collection();
        this.client.on('ready', ()=> {
            this.channels = this.config.channels.map(ch => {
                let channel = this.client.channels.cache.get(ch.id);
                if(!this.guilds.find(guild => guild.id == channel.guild.id)){
                    this.guilds.push(channel.guild);
                }
                return channel;
            });
            this.event.emit('ready');
        });
        this.event = new Event();

        this.registerIntervalId = setInterval(()=>{
            this.rest.put(
                Routes.applicationCommands(this.config.clientId),
                { 
                    'body': this.client.commands.map(c => c.data.toJSON())
                }
            );
            console.log("registerd");
        }, config.Discord.commandRegisterInterval);
    }
    async send(msg, index = null){
        if(index){
            await this.channels[index].send(msg);
        }
        else{
            for(let i = 0; i < this.channels.length; i++){
                await this.channels[i].send(msg);
            };
        }
    }
    async obeyCommand(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            const discord_slash_command = {
                'data': new SlashCommandBuilder().setName(command.name).setDescription(command.description),
                'execute': async (interaction, edit_reply) =>{
                    let reply_msg
                    try{
                        reply_msg = await callback();
                    }
                    catch(error){
                        console.error("List hat nicht funktioniert", error);
                        reply_msg = error;
                    }
                    if(edit_reply){
                        interaction.reply(reply_msg);
                    }
                    else{
                        interaction.editReply(reply_msg);
                    }
                }
            };
            this.client.commands.set(discord_slash_command.data.name, discord_slash_command);
            this.client.on(Events.InteractionCreate, async interaction => {
                if(interaction.isChatInputCommand()){
                    if(interaction.commandName == command_name){
                        if(this.channels.find(ch => ch.id == interaction.channel.id)){
                            const slash_command = this.client.commands.get(interaction.commandName);
                            if(slash_command){
                                let command_reply = command.reply(interaction.user.username);
                                if(command_reply != null){
                                    await interaction.reply(command_reply);
                                }
                                try{
                                    if(callback){
                                        slash_command.execute(interaction, command_reply == null);
                                    }
                                }
                                catch(error){
                                    console.error(error);
                                    await interaction.reply({
                                        'content': "There was an error while executing this command!",
                                        'ephemeral': true
                                    });
                                }
                            }
                            else{
                                try {
                                    await interaction.reply({
                                        'content': "This command is not supported",
                                        'ephemeral': true
                                    });
                                }
                                catch(error){
                                    console.error(error);
                                }
                            }
                        }
                        else{
                            setTimeout(async ()=>{
                                try{
                                    await interaction.reply({
                                        'content': "The Server that is linked to this channel is not online currently!\nVisit https://mc.dalbudak.de to start your server.",
                                        'ephemeral': true
                                    });
                                }
                                catch(error){
                                    console.error(error);
                                }
                            }, 6000);
                        }
                    }
                }
            });
            this.commands_obeyed_counter++;
            if(this.commands_obeyed_counter == 1){
                this.obeyCommand('help');
            }
        }
        else{
            throw new Error('Obey command not supported');
        }
    }
    // OBEY COMMAND WITHOUT SLASH COMMAND SUPPORT
    obeyCommandText(command_name, callback){
        const command = supported_commands.find(e => e.name === command_name);
        if(command){
            command.active = true;
            this.client.on('messageCreate', async (message) => {
                if (this.config.channels.find(ch => ch.id == message.channel.id)) {
                    if(message.content === config.Discord.commandPrefix + command_name){
                        message.react("👍");
                        let command_reply = command.reply(message.author.username);
                        if(command_reply != null){
                            message.channel.send(command_reply);
                        }
                        try{
                            if(callback){
                                let reply_msg = await callback();
                                console.log(reply_msg);
                                message.channel.send(reply_msg);
                            }
                        }
                        catch(err){
                            console.error("Couldn't reply to users request", err);
                        }
                    }
                }
            });
            this.commands_obeyed_counter++;
            if(this.commands_obeyed_counter == 1){
                this.obeyCommand('help');
            }
        }
        else{
            throw new Error('Obey command not supported');
        }
    }
    logout(){
        clearInterval(this.registerIntervalId);
        this.client.destroy()
    }
    on(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.on(name, callback);
    }
    once(name, callback){ // THIS IS A SHORTCUT FOR ADDING AN EVENT LISTENER
        this.event.once(name, callback);
    }
    // GETTER
    get commands_obeyed(){
        return supported_commands.reduce((acc, curr) => {
            if(curr.active)
                acc.push(curr.name);
            return acc;
        }, []);
    }
};