// SPDX-License-Identifier: AGPL-3.0-or-later

// LALR(1) grammar for COOL in the style of the course's Bison cool.y.
// Regenerate the committed parser on a development machine with:
//
//	make generate-frontend
//
// which runs goyacc (golang.org/x/tools/cmd/goyacc, pinned in the Makefile)
// at build time only. Nothing is generated at runtime.
//
// Conflicts: none. The grammar uses yacc precedence declarations for every
// operator, LET_STMT for the "let extends as far right as possible" rule, and
// the classic error productions (class, feature, block statement, and let
// binding) for recovery. See docs/course-assignments.md#front-end-equivalence.

%{
package compiler
%}

%union {
	tok      Token
	expr     Expr
	exprs    []Expr
	feature  Feature
	features []Feature
	class    *ClassDecl
	classes  []*ClassDecl
	formal   Formal
	formals  []Formal
	binding  LetBinding
	bindings []LetBinding
	branch   CaseBranch
	branches []CaseBranch
	// loc is the course parser's location (curr_lineno of the token). The
	// generated parser copies $1 into $$ before every action, so loc follows
	// Bison's YYLLOC_DEFAULT (Current = Rhs[1]) without any explicit code.
	loc int
}

%token <tok> CLASS ELSE FI IF IN INHERITS LET LOOP POOL THEN WHILE CASE ESAC OF DARROW NEW ISVOID
%token <tok> STR_CONST INT_CONST BOOL_CONST TYPEID OBJECTID ASSIGN NOT LE ERROR LET_STMT
%token <tok> '{' '}' '(' ')' ':' ';' ',' '.' '@' '+' '-' '*' '/' '~' '<' '='

%type <classes> class_list
%type <class> class
%type <features> feature_list
%type <feature> feature
%type <formals> formal_list formals
%type <formal> formal
%type <expr> expr let_body
%type <exprs> block_list arg_list args
%type <bindings> let_bindings
%type <binding> let_binding
%type <branches> case_list
%type <branch> case_branch

%nonassoc LET_STMT
%right ASSIGN
%left NOT
%nonassoc LE '<' '='
%left '+' '-'
%left '*' '/'
%left ISVOID
%left '~'
%left '@'
%left '.'

%%

program
	: class_list
		{ coolyylex.(*lalrLexer).program = &Program{Classes: $1} }
	;

class_list
	: class
		{ $$ = appendClass(nil, $1) }
	| class_list class
		{ $$ = appendClass($1, $2) }
	;

class
	: CLASS TYPEID '{' feature_list '}' ';'
		{ $$ = &ClassDecl{Name: $2.Lexeme, Parent: "Object", Features: $4, NameSpan: $2.Span, ParentSpan: $2.Span, Span: joinSpans($1.Span, $6.Span)} }
	| CLASS TYPEID INHERITS TYPEID '{' feature_list '}' ';'
		{ $$ = &ClassDecl{Name: $2.Lexeme, Parent: $4.Lexeme, Features: $6, NameSpan: $2.Span, ParentSpan: $4.Span, Span: joinSpans($1.Span, $8.Span)} }
	| error ';'
		{ $$ = nil }
	;

feature_list
	: /* empty */
		{ $$ = nil }
	| feature_list feature ';'
		{ $$ = append($1, withFeatureEnd($2, $3.Span)) }
	| feature_list error ';'
		{ $$ = $1 }
	;

feature
	: OBJECTID '(' formal_list ')' ':' TYPEID '{' expr '}'
		{ $$ = &MethodDecl{Name: $1.Lexeme, Formals: $3, ReturnType: $6.Lexeme, Body: $8, NameSpan: $1.Span, TypeSpan: $6.Span, Span: joinSpans($1.Span, $9.Span)} }
	| OBJECTID ':' TYPEID
		{ $$ = &AttributeDecl{Name: $1.Lexeme, TypeName: $3.Lexeme, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $3.Span)} }
	| OBJECTID ':' TYPEID ASSIGN expr
		{ $$ = &AttributeDecl{Name: $1.Lexeme, TypeName: $3.Lexeme, Init: $5, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $5.GetSpan())} }
	;

formal_list
	: /* empty */
		{ $$ = []Formal{} }
	| formals
		{ $$ = $1 }
	;

formals
	: formal
		{ $$ = []Formal{$1} }
	| formals ',' formal
		{ $$ = append($1, $3) }
	;

formal
	: OBJECTID ':' TYPEID
		{ $$ = Formal{Name: $1.Lexeme, TypeName: $3.Lexeme, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $3.Span)} }
	;

expr
	: OBJECTID ASSIGN expr
		{ $$ = coolyylex.(*lalrLexer).node(&AssignExpr{exprBase: exprBase{Span: joinSpans($1.Span, $3.GetSpan())}, Name: $1.Lexeme, NameSpan: $1.Span, Value: $3}, coolyyVAL.loc) }
	| expr '@' TYPEID '.' OBJECTID '(' arg_list ')'
		{ $$ = coolyylex.(*lalrLexer).node(&DispatchExpr{exprBase: exprBase{Span: joinSpans($1.GetSpan(), $8.Span)}, Receiver: $1, StaticTypeName: $3.Lexeme, StaticTypeSpan: $3.Span, Method: $5.Lexeme, MethodSpan: $5.Span, Arguments: $7}, coolyyVAL.loc) }
	| expr '.' OBJECTID '(' arg_list ')'
		{ $$ = coolyylex.(*lalrLexer).node(&DispatchExpr{exprBase: exprBase{Span: joinSpans($1.GetSpan(), $6.Span)}, Receiver: $1, Method: $3.Lexeme, MethodSpan: $3.Span, Arguments: $5}, coolyyVAL.loc) }
	| OBJECTID '(' arg_list ')'
		{ $$ = coolyylex.(*lalrLexer).node(&DispatchExpr{exprBase: exprBase{Span: joinSpans($1.Span, $4.Span)}, Method: $1.Lexeme, MethodSpan: $1.Span, Arguments: $3}, coolyyVAL.loc) }
	| IF expr THEN expr ELSE expr FI
		{ $$ = coolyylex.(*lalrLexer).node(&IfExpr{exprBase: exprBase{Span: joinSpans($1.Span, $7.Span)}, Condition: $2, Then: $4, Else: $6}, coolyyVAL.loc) }
	| WHILE expr LOOP expr POOL
		{ $$ = coolyylex.(*lalrLexer).node(&WhileExpr{exprBase: exprBase{Span: joinSpans($1.Span, $5.Span)}, Condition: $2, Body: $4}, coolyyVAL.loc) }
	| '{' block_list '}'
		{ $$ = coolyylex.(*lalrLexer).node(&BlockExpr{exprBase: exprBase{Span: joinSpans($1.Span, $3.Span)}, Expressions: $2}, coolyyVAL.loc) }
	| LET let_bindings IN let_body
		{ $$ = coolyylex.(*lalrLexer).node(&LetExpr{exprBase: exprBase{Span: joinSpans($1.Span, $4.GetSpan())}, Bindings: $2, Body: $4}, coolyyVAL.loc) }
	| CASE expr OF case_list ESAC
		{ $$ = coolyylex.(*lalrLexer).node(&CaseExpr{exprBase: exprBase{Span: joinSpans($1.Span, $5.Span)}, Value: $2, Branches: $4}, coolyyVAL.loc) }
	| NEW TYPEID
		{ $$ = coolyylex.(*lalrLexer).node(&NewExpr{exprBase: exprBase{Span: joinSpans($1.Span, $2.Span)}, TypeName: $2.Lexeme, TypeSpan: $2.Span}, coolyyVAL.loc) }
	| ISVOID expr
		{ $$ = coolyylex.(*lalrLexer).node(&IsVoidExpr{exprBase: exprBase{Span: joinSpans($1.Span, $2.GetSpan())}, Value: $2}, coolyyVAL.loc) }
	| expr '+' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenPlus, $3), coolyyVAL.loc) }
	| expr '-' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenMinus, $3), coolyyVAL.loc) }
	| expr '*' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenStar, $3), coolyyVAL.loc) }
	| expr '/' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenSlash, $3), coolyyVAL.loc) }
	| '~' expr
		{ $$ = coolyylex.(*lalrLexer).node(&UnaryExpr{exprBase: exprBase{Span: joinSpans($1.Span, $2.GetSpan())}, Operator: TokenTilde, OpSpan: $1.Span, Value: $2}, coolyyVAL.loc) }
	| expr '<' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenLess, $3), coolyyVAL.loc) }
	| expr LE expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenLessEqual, $3), coolyyVAL.loc) }
	| expr '=' expr
		{ $$ = coolyylex.(*lalrLexer).node(binaryExpr($1, $2, TokenEqual, $3), coolyyVAL.loc) }
	| NOT expr
		{ $$ = coolyylex.(*lalrLexer).node(&UnaryExpr{exprBase: exprBase{Span: joinSpans($1.Span, $2.GetSpan())}, Operator: TokenNot, OpSpan: $1.Span, Value: $2}, coolyyVAL.loc) }
	| '(' expr ')'
		{ $$ = groupExpr($2, $1.Span, $3.Span) }
	| OBJECTID
		{ $$ = coolyylex.(*lalrLexer).node(&IdentifierExpr{exprBase: exprBase{Span: $1.Span}, Name: $1.Lexeme, NameSpan: $1.Span}, coolyyVAL.loc) }
	| INT_CONST
		{ $$ = coolyylex.(*lalrLexer).node(coolyylex.(*lalrLexer).intExpr($1), coolyyVAL.loc) }
	| STR_CONST
		{ $$ = coolyylex.(*lalrLexer).node(&StringExpr{exprBase: exprBase{Span: $1.Span}, Value: $1.Literal}, coolyyVAL.loc) }
	| BOOL_CONST
		{ $$ = coolyylex.(*lalrLexer).node(&BoolExpr{exprBase: exprBase{Span: $1.Span}, Value: $1.Literal == "true"}, coolyyVAL.loc) }
	;

let_body
	: expr %prec LET_STMT
		{ $$ = $1 }
	;

let_bindings
	: let_binding
		{ $$ = []LetBinding{$1} }
	| let_bindings ',' let_binding
		{ $$ = append($1, $3) }
	| error ',' let_binding
		{ $$ = []LetBinding{$3} }
	| let_bindings ',' error
		{ $$ = $1 }
	;

let_binding
	: OBJECTID ':' TYPEID
		{ $$ = LetBinding{Name: $1.Lexeme, TypeName: $3.Lexeme, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $3.Span)} }
	| OBJECTID ':' TYPEID ASSIGN expr
		{ $$ = LetBinding{Name: $1.Lexeme, TypeName: $3.Lexeme, Init: $5, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $5.GetSpan())} }
	;

case_list
	: case_branch
		{ $$ = []CaseBranch{$1} }
	| case_list case_branch
		{ $$ = append($1, $2) }
	;

case_branch
	: OBJECTID ':' TYPEID DARROW expr ';'
		{ $$ = CaseBranch{Name: $1.Lexeme, TypeName: $3.Lexeme, Body: $5, NameSpan: $1.Span, TypeSpan: $3.Span, Span: joinSpans($1.Span, $6.Span)} }
	;

block_list
	: expr ';'
		{ $$ = []Expr{$1} }
	| block_list expr ';'
		{ $$ = append($1, $2) }
	| error ';'
		{ $$ = []Expr{} }
	| block_list error ';'
		{ $$ = $1 }
	;

arg_list
	: /* empty */
		{ $$ = []Expr{} }
	| args
		{ $$ = $1 }
	;

args
	: expr
		{ $$ = []Expr{$1} }
	| args ',' expr
		{ $$ = append($1, $3) }
	;

%%
