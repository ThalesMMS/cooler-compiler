(* Outer comment (* with a nested comment *) remains ignored. *)
cLaSs Main InHeRiTs IO {
  value : Int <- 1;
  main() : Object {
    {
      if tRuE then out_string("lexer\tok\n") else out_string("bad") fi;
      if fAlSe then out_string("bad") else out_string("!") fi;
    }
  };
};
